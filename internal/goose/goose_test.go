package goose

import (
	"bytes"
	"errors"
	"fmt"
	"go/build"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestGoose(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	const testRoot = "testdata"
	testdataEnts, err := ioutil.ReadDir(testRoot) // ReadDir sorts by name
	if err != nil {
		t.Fatal(err)
	}
	tests := make([]*testCase, 0, len(testdataEnts))
	for _, ent := range testdataEnts {
		name := ent.Name()
		if !ent.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		test, err := loadTestCase(filepath.Join(testRoot, name))
		if err != nil {
			t.Error(err)
		}
		tests = append(tests, test)
	}

	t.Run("Generate", func(t *testing.T) {
		if _, err := os.Stat(filepath.Join(build.Default.GOROOT, "bin", "go")); err != nil {
			t.Skip("go toolchain not available:", err)
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				bctx := test.buildContext()
				gen, err := Generate(bctx, wd, test.pkg)
				if len(gen) > 0 {
					defer t.Logf("goose_gen.go:\n%s", gen)
				}
				if err != nil {
					if !test.wantError {
						t.Fatalf("goose: %v", err)
					}
					return
				}
				if err == nil && test.wantError {
					t.Fatal("goose succeeded; want error")
				}

				gopath, err := ioutil.TempDir("", "goose_test")
				if err != nil {
					t.Fatal(err)
				}
				defer os.RemoveAll(gopath)
				if err := test.materialize(gopath); err != nil {
					t.Fatal(err)
				}
				if len(gen) > 0 {
					genPath := filepath.Join(gopath, "src", filepath.FromSlash(test.pkg), "goose_gen.go")
					if err := ioutil.WriteFile(genPath, gen, 0666); err != nil {
						t.Fatal(err)
					}
				}
				testExePath := filepath.Join(gopath, "bin", "testprog")
				realBuildCtx := &build.Context{
					GOARCH:      bctx.GOARCH,
					GOOS:        bctx.GOOS,
					GOROOT:      bctx.GOROOT,
					GOPATH:      gopath,
					CgoEnabled:  bctx.CgoEnabled,
					Compiler:    bctx.Compiler,
					BuildTags:   bctx.BuildTags,
					ReleaseTags: bctx.ReleaseTags,
				}
				if err := runGo(realBuildCtx, "build", "-o", testExePath, test.pkg); err != nil {
					t.Fatal("build:", err)
				}
				out, err := exec.Command(testExePath).Output()
				if err != nil {
					t.Fatal("run compiled program:", err)
				}
				if !bytes.Equal(out, test.wantOutput) {
					t.Errorf("compiled program output = %q; want %q", out, test.wantOutput)
				}
			})
		}
	})

	t.Run("Determinism", func(t *testing.T) {
		runs := 10
		if testing.Short() {
			runs = 3
		}
		for _, test := range tests {
			if test.wantError {
				continue
			}
			t.Run(test.name, func(t *testing.T) {
				bctx := test.buildContext()
				gold, err := Generate(bctx, wd, test.pkg)
				if err != nil {
					t.Fatal("goose:", err)
				}
				goldstr := string(gold)
				for i := 0; i < runs-1; i++ {
					out, err := Generate(bctx, wd, test.pkg)
					if err != nil {
						t.Fatal("goose (on subsequent run):", err)
					}
					if !bytes.Equal(gold, out) {
						t.Fatalf("goose output differs when run repeatedly on same input:\n%s", diff(goldstr, string(out)))
					}
				}
			})
		}
	})
}

type testCase struct {
	name       string
	pkg        string
	goFiles    map[string][]byte
	wantOutput []byte
	wantError  bool
}

// loadTestCase reads a test case from a directory.
//
// The directory structure is:
//
//	root/
//		pkg        file containing the package name containing the inject function
//		           (must also be package main)
//		out.txt    file containing the expected output, or the magic string "ERROR"
//		           if this test should cause generation to fail
//		...        any Go files found recursively placed under GOPATH/src/...
func loadTestCase(root string) (*testCase, error) {
	name := filepath.Base(root)
	pkg, err := ioutil.ReadFile(filepath.Join(root, "pkg"))
	if err != nil {
		return nil, fmt.Errorf("load test case %s: %v", name, err)
	}
	out, err := ioutil.ReadFile(filepath.Join(root, "out.txt"))
	if err != nil {
		return nil, fmt.Errorf("load test case %s: %v", name, err)
	}
	wantError := false
	if bytes.Equal(bytes.TrimSpace(out), []byte("ERROR")) {
		wantError = true
		out = nil
	}
	goFiles := make(map[string][]byte)
	err = filepath.Walk(root, func(src string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || filepath.Ext(src) != ".go" {
			return nil
		}
		rel, err := filepath.Rel(root, src)
		if err != nil {
			return err // unlikely
		}
		data, err := ioutil.ReadFile(src)
		if err != nil {
			return err
		}
		goFiles[rel] = data
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load test case %s: %v", name, err)
	}
	return &testCase{
		name:       name,
		pkg:        string(bytes.TrimSpace(pkg)),
		goFiles:    goFiles,
		wantOutput: out,
		wantError:  wantError,
	}, nil
}

func (test *testCase) buildContext() *build.Context {
	return &build.Context{
		GOARCH:      build.Default.GOARCH,
		GOOS:        build.Default.GOOS,
		GOROOT:      build.Default.GOROOT,
		GOPATH:      magicGOPATH(),
		CgoEnabled:  build.Default.CgoEnabled,
		Compiler:    build.Default.Compiler,
		ReleaseTags: build.Default.ReleaseTags,
		HasSubdir:   test.hasSubdir,
		ReadDir:     test.readDir,
		OpenFile:    test.openFile,
		IsDir:       test.isDir,
	}
}

const (
	magicGOPATHUnix    = "/goose_gopath"
	magicGOPATHWindows = `C:\goose_gopath`
)

func magicGOPATH() string {
	if runtime.GOOS == "windows" {
		return magicGOPATHWindows
	} else {
		return magicGOPATHUnix
	}
}

func (test *testCase) hasSubdir(root, dir string) (rel string, ok bool) {
	// Don't consult filesystem, just lexical.

	if dir == root {
		return "", true
	}
	prefix := root
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	if !strings.HasPrefix(dir, prefix) {
		return "", false
	}
	return filepath.ToSlash(dir[len(prefix):]), true
}

func (test *testCase) resolve(path string) (resolved string, pathType int) {
	subpath, isMagic := test.hasSubdir(magicGOPATH(), path)
	if !isMagic {
		return path, systemPath
	}
	if subpath == "src" {
		return "", gopathRoot
	}
	const srcPrefix = "src/"
	if !strings.HasPrefix(subpath, srcPrefix) {
		return subpath, gopathRoot
	}
	return subpath[len(srcPrefix):], gopathSrc
}

// Path types
const (
	systemPath = iota
	gopathRoot
	gopathSrc
)

func (test *testCase) readDir(dir string) ([]os.FileInfo, error) {
	rpath, pathType := test.resolve(dir)
	switch {
	case pathType == systemPath:
		return ioutil.ReadDir(rpath)
	case pathType == gopathRoot && rpath == "":
		return []os.FileInfo{dirInfo{name: "src"}}, nil
	case pathType == gopathSrc:
		names := make([]string, 0, len(test.goFiles))
		prefix := rpath + string(filepath.Separator)
		for name := range test.goFiles {
			if strings.HasPrefix(name, prefix) {
				names = append(names, name[len(prefix):])
			}
		}
		sort.Strings(names)
		ents := make([]os.FileInfo, 0, len(names))
		for _, name := range names {
			if i := strings.IndexRune(name, filepath.Separator); i != -1 {
				// Directory
				dirName := name[:i]
				if len(ents) == 0 || ents[len(ents)-1].Name() != dirName {
					ents = append(ents, dirInfo{name: dirName})
				}
				continue
			}
			ents = append(ents, fileInfo{
				name: name,
				size: int64(len(test.goFiles[name])),
			})
		}
		return ents, nil
	default:
		return nil, &os.PathError{
			Op:   "open",
			Path: dir,
			Err:  os.ErrNotExist,
		}
	}
}

func (test *testCase) isDir(path string) bool {
	rpath, pathType := test.resolve(path)
	switch {
	case pathType == systemPath:
		info, err := os.Stat(rpath)
		return err == nil && info.IsDir()
	case pathType == gopathRoot && rpath == "":
		return true
	case pathType == gopathSrc:
		prefix := rpath + string(filepath.Separator)
		for name := range test.goFiles {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

type dirInfo struct {
	name string
}

func (d dirInfo) Name() string       { return d.name }
func (d dirInfo) Size() int64        { return 0 }
func (d dirInfo) Mode() os.FileMode  { return os.ModeDir | 0777 }
func (d dirInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (d dirInfo) IsDir() bool        { return true }
func (d dirInfo) Sys() interface{}   { return nil }

type fileInfo struct {
	name string
	size int64
}

func (f fileInfo) Name() string       { return f.name }
func (f fileInfo) Size() int64        { return f.size }
func (f fileInfo) Mode() os.FileMode  { return os.ModeDir | 0666 }
func (f fileInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (f fileInfo) IsDir() bool        { return false }
func (f fileInfo) Sys() interface{}   { return nil }

func (test *testCase) openFile(path string) (io.ReadCloser, error) {
	rpath, pathType := test.resolve(path)
	switch {
	case pathType == systemPath:
		return os.Open(path)
	case pathType == gopathSrc:
		content, ok := test.goFiles[rpath]
		if !ok {
			return nil, &os.PathError{
				Op:   "open",
				Path: path,
				Err:  errors.New("does not exist or is not a file"),
			}
		}
		return ioutil.NopCloser(bytes.NewReader(content)), nil
	default:
		return nil, &os.PathError{
			Op:   "open",
			Path: path,
			Err:  errors.New("does not exist or is not a file"),
		}
	}
}

// materialize creates a new GOPATH at the given directory, which may or
// may not exist.
func (test *testCase) materialize(gopath string) error {
	for name, content := range test.goFiles {
		dst := filepath.Join(gopath, "src", name)
		if err := os.MkdirAll(filepath.Dir(dst), 0777); err != nil {
			return fmt.Errorf("materialize GOPATH: %v", err)
		}
		if err := ioutil.WriteFile(dst, content, 0666); err != nil {
			return fmt.Errorf("materialize GOPATH: %v", err)
		}
	}
	return nil
}

func runGo(bctx *build.Context, args ...string) error {
	exe := filepath.Join(bctx.GOROOT, "bin", "go")
	c := exec.Command(exe, args...)
	c.Env = append(os.Environ(), "GOROOT="+bctx.GOROOT, "GOARCH="+bctx.GOARCH, "GOOS="+bctx.GOOS, "GOPATH="+bctx.GOPATH)
	if bctx.CgoEnabled {
		c.Env = append(c.Env, "CGO_ENABLED=1")
	} else {
		c.Env = append(c.Env, "CGO_ENABLED=0")
	}
	// TODO(someday): set -compiler flag if needed.
	out, err := c.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return fmt.Errorf("%v; output:\n%s", err, out)
		}
		return err
	}
	return nil
}

func diff(want, got string) string {
	d, err := runDiff([]byte(want), []byte(got))
	if err == nil {
		return string(d)
	}
	return "*** got:\n" + got + "\n\n*** want:\n" + want
}

func runDiff(a, b []byte) ([]byte, error) {
	fa, err := ioutil.TempFile("", "goose_test_diff")
	if err != nil {
		return nil, err
	}
	defer func() {
		os.Remove(fa.Name())
		fa.Close()
	}()
	fb, err := ioutil.TempFile("", "goose_test_diff")
	if err != nil {
		return nil, err
	}
	defer func() {
		os.Remove(fb.Name())
		fb.Close()
	}()
	if _, err := fa.Write(a); err != nil {
		return nil, err
	}
	if _, err := fb.Write(b); err != nil {
		return nil, err
	}
	c := exec.Command("diff", "-u", fa.Name(), fb.Name())
	out, err := c.Output()
	return out, err
}

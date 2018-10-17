// Copyright 2018 The Go Cloud Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package wire

import (
	"bytes"
	"errors"
	"fmt"
	"go/build"
	"go/types"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/go-cloud/internal/testing/setup"
	"github.com/google/go-cmp/cmp"
)

func TestWire(t *testing.T) {
	const testRoot = "testdata"
	testdataEnts, err := ioutil.ReadDir(testRoot) // ReadDir sorts by name.
	if err != nil {
		t.Fatal(err)
	}
	// The marker function package source is needed to have the test cases
	// type check. loadTestCase places this file at the well-known import path.
	wireGo, err := ioutil.ReadFile(filepath.Join("..", "..", "wire.go"))
	if err != nil {
		t.Fatal(err)
	}
	tests := make([]*testCase, 0, len(testdataEnts))
	for _, ent := range testdataEnts {
		name := ent.Name()
		if !ent.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		test, err := loadTestCase(filepath.Join(testRoot, name), wireGo)
		if err != nil {
			t.Error(err)
			continue
		}
		tests = append(tests, test)
	}
	wd := filepath.Join(magicGOPATH(), "src")

	if *setup.Record {
		if _, err := os.Stat(filepath.Join(build.Default.GOROOT, "bin", "go")); err != nil {
			t.Fatal("go toolchain not available:", err)
		}
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Run Wire from a fake build context.
			bctx := test.buildContext()
			gen, errs := Generate(bctx, wd, test.pkg)
			if len(gen) > 0 {
				defer t.Logf("wire_gen.go:\n%s", gen)
			}
			if len(errs) > 0 {
				gotErrStrings := make([]string, len(errs))
				for i, e := range errs {
					gotErrStrings[i] = scrubError(e.Error())
					t.Log(gotErrStrings[i])
				}
				if !test.wantWireError {
					t.Fatal("Did not expect errors. To -record an error, create want/wire_errs.txt.")
				}
				if *setup.Record {
					wireErrsFile := filepath.Join(testRoot, test.name, "want", "wire_errs.txt")
					if err := ioutil.WriteFile(wireErrsFile, []byte(strings.Join(gotErrStrings, "\n\n")), 0666); err != nil {
						t.Fatalf("failed to write wire_errs.txt file: %v", err)
					}
				} else {
					if diff := cmp.Diff(gotErrStrings, test.wantWireErrorStrings); diff != "" {
						t.Errorf("Errors didn't match expected errors from wire_errors.txt:\n%s", diff)
					}
				}
				return
			}
			if test.wantWireError {
				t.Fatal("wire succeeded; want error")
			}

			if *setup.Record {
				// Record ==> Build the generated Wire code,
				// check that the program's output matches the
				// expected output, save wire output on
				// success.
				if err := goBuildCheck(test, wd, bctx, gen); err != nil {
					t.Fatalf("go build check failed: %v", err)
				}
				wireGenFile := filepath.Join(testRoot, test.name, "want", "wire_gen.go")
				if err := ioutil.WriteFile(wireGenFile, gen, 0666); err != nil {
					t.Fatalf("failed to write wire_gen.go file: %v", err)
				}
			} else {
				// Replay ==> Load golden file and compare to
				// generated result. This check is meant to
				// detect non-deterministic behavior in the
				// Generate function.
				if !bytes.Equal(gen, test.wantWireOutput) {
					gotS, wantS := string(gen), string(test.wantWireOutput)
					diff := cmp.Diff(strings.Split(gotS, "\n"), strings.Split(wantS, "\n"))
					t.Fatalf("wire output differs from golden file. If this change is expected, run with -record to update the wire_gen.go file.\n*** got:\n%s\n\n*** want:\n%s\n\n*** diff:\n%s", gotS, wantS, diff)
				}
			}
		})
	}
}

func goBuildCheck(test *testCase, wd string, bctx *build.Context, gen []byte) error {
	// Find the absolute import path, since test.pkg may be a relative
	// import path.
	genPkg, err := bctx.Import(test.pkg, wd, build.FindOnly)
	if err != nil {
		return err
	}

	// Run a `go build` with the generated output.
	gopath, err := ioutil.TempDir("", "wire_test")
	if err != nil {
		return err
	}
	defer os.RemoveAll(gopath)
	if err := test.materialize(gopath); err != nil {
		return err
	}
	if len(gen) > 0 {
		genPath := filepath.Join(gopath, "src", filepath.FromSlash(genPkg.ImportPath), "wire_gen.go")
		if err := ioutil.WriteFile(genPath, gen, 0666); err != nil {
			return err
		}
	}
	if err := writeGoMod(gopath); err != nil {
		return err
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
	buildDir := filepath.Join(gopath, "src", genPkg.ImportPath)
	buildCmd := []string{"build", "-o", testExePath}
	if test.name == "Vendor" && os.Getenv("GO111MODULE") == "on" {
		buildCmd = append(buildCmd, "-mod=vendor")
	}
	if err := runGo(realBuildCtx, buildDir, buildCmd...); err != nil {
		return fmt.Errorf("build: %v", err)
	}

	// Run the resulting program and compare its output to the expected
	// output.
	out, err := exec.Command(testExePath).Output()
	if err != nil {
		return fmt.Errorf("run compiled program: %v", err)
	}
	if !bytes.Equal(out, test.wantProgramOutput) {
		gotS, wantS := string(out), string(test.wantProgramOutput)
		diff := cmp.Diff(strings.Split(gotS, "\n"), strings.Split(wantS, "\n"))
		return fmt.Errorf("compiled program output doesn't match:\n*** got:\n%s\n\n*** want:\n%s\n\n*** diff:\n%s", gotS, wantS, diff)
	}
	return nil
}

func TestUnexport(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"", ""},
		{"a", "a"},
		{"ab", "ab"},
		{"A", "a"},
		{"AB", "ab"},
		{"A_", "a_"},
		{"ABc", "aBc"},
		{"ABC", "abc"},
		{"AB_", "ab_"},
		{"foo", "foo"},
		{"Foo", "foo"},
		{"HTTPClient", "httpClient"},
		{"IFace", "iFace"},
		{"SNAKE_CASE", "snake_CASE"},
		{"HTTP", "http"},
	}
	for _, test := range tests {
		if got := unexport(test.name); got != test.want {
			t.Errorf("unexport(%q) = %q; want %q", test.name, got, test.want)
		}
	}
}

func TestExport(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"", ""},
		{"a", "A"},
		{"ab", "Ab"},
		{"A", "A"},
		{"AB", "AB"},
		{"A_", "A_"},
		{"ABc", "ABc"},
		{"ABC", "ABC"},
		{"AB_", "AB_"},
		{"foo", "Foo"},
		{"Foo", "Foo"},
		{"HTTPClient", "HTTPClient"},
		{"httpClient", "HttpClient"},
		{"IFace", "IFace"},
		{"iFace", "IFace"},
		{"SNAKE_CASE", "SNAKE_CASE"},
		{"HTTP", "HTTP"},
	}
	for _, test := range tests {
		if got := export(test.name); got != test.want {
			t.Errorf("export(%q) = %q; want %q", test.name, got, test.want)
		}
	}
}

func TestTypeVariableName(t *testing.T) {
	var (
		boolT           = types.Typ[types.Bool]
		stringT         = types.Typ[types.String]
		fooVarT         = types.NewNamed(types.NewTypeName(0, nil, "foo", stringT), stringT, nil)
		nonameVarT      = types.NewNamed(types.NewTypeName(0, nil, "", stringT), stringT, nil)
		barVarInFooPkgT = types.NewNamed(types.NewTypeName(0, types.NewPackage("my.example/foo", "foo"), "bar", stringT), stringT, nil)
	)
	tests := []struct {
		description     string
		typ             types.Type
		defaultName     string
		transformAppend string
		collides        map[string]bool
		want            string
	}{
		{"basic type", boolT, "", "", map[string]bool{}, "bool"},
		{"basic type with transform", boolT, "", "suffix", map[string]bool{}, "boolsuffix"},
		{"basic type with collision", boolT, "", "", map[string]bool{"bool": true}, "bool2"},
		{"basic type with transform and collision", boolT, "", "suffix", map[string]bool{"boolsuffix": true}, "boolsuffix2"},
		{"a different basic type", stringT, "", "", map[string]bool{}, "string"},
		{"named type", fooVarT, "", "", map[string]bool{}, "foo"},
		{"named type with transform", fooVarT, "", "suffix", map[string]bool{}, "foosuffix"},
		{"named type with collision", fooVarT, "", "", map[string]bool{"foo": true}, "foo2"},
		{"named type with transform and collision", fooVarT, "", "suffix", map[string]bool{"foosuffix": true}, "foosuffix2"},
		{"noname type", nonameVarT, "bar", "", map[string]bool{}, "bar"},
		{"noname type with transform", nonameVarT, "bar", "s", map[string]bool{}, "bars"},
		{"noname type with transform and collision", nonameVarT, "bar", "s", map[string]bool{"bars": true}, "bars2"},
		{"var in pkg type", barVarInFooPkgT, "", "", map[string]bool{}, "bar"},
		{"var in pkg type with collision", barVarInFooPkgT, "", "", map[string]bool{"bar": true}, "fooBar"},
		{"var in pkg type with double collision", barVarInFooPkgT, "", "", map[string]bool{"bar": true, "fooBar": true}, "bar2"},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s: typeVariableName(%v, %q, %q, %v)", test.description, test.typ, test.defaultName, test.transformAppend, test.collides), func(t *testing.T) {
			got := typeVariableName(test.typ, test.defaultName, func(name string) string { return name + test.transformAppend }, func(name string) bool { return test.collides[name] })
			if !isIdent(got) {
				t.Errorf("%q is not an identifier", got)
			}
			if got != test.want {
				t.Errorf("got %q want %q", got, test.want)
			}
			if test.collides[got] {
				t.Errorf("%q collides", got)
			}
		})
	}
}

func TestDisambiguate(t *testing.T) {
	tests := []struct {
		name     string
		want     string
		collides map[string]bool
	}{
		{"foo", "foo", nil},
		{"foo", "foo2", map[string]bool{"foo": true}},
		{"foo", "foo3", map[string]bool{"foo": true, "foo1": true, "foo2": true}},
		{"foo1", "foo1_2", map[string]bool{"foo": true, "foo1": true, "foo2": true}},
		{"foo\u0661", "foo\u0661", map[string]bool{"foo": true, "foo1": true, "foo2": true}},
		{"foo\u0661", "foo\u06612", map[string]bool{"foo": true, "foo1": true, "foo2": true, "foo\u0661": true}},
		{"select", "select2", nil},
		{"var", "var2", nil},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("disambiguate(%q, %v)", test.name, test.collides), func(t *testing.T) {
			got := disambiguate(test.name, func(name string) bool { return test.collides[name] })
			if !isIdent(got) {
				t.Errorf("%q is not an identifier", got)
			}
			if got != test.want {
				t.Errorf("got %q want %q", got, test.want)
			}
			if test.collides[got] {
				t.Errorf("%q collides", got)
			}
		})
	}
}

func isIdent(s string) bool {
	if len(s) == 0 {
		if s == "foo" {
			panic("BREAK3")
		}
		return false
	}
	r, i := utf8.DecodeRuneInString(s)
	if !unicode.IsLetter(r) && r != '_' {
		if s == "foo" {
			panic("BREAK2")
		}
		return false
	}
	for i < len(s) {
		r, sz := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			if s == "foo" {
				panic("BREAK1")
			}
			return false
		}
		i += sz
	}
	return true
}

type testCase struct {
	name                 string
	pkg                  string
	goFiles              map[string][]byte
	wantProgramOutput    []byte
	wantWireOutput       []byte
	wantWireError        bool
	wantWireErrorStrings []string
}

var scrubLineNumberAndPositionRegex = regexp.MustCompile("\\.go:[\\d]+:[\\d]+")
var scrubLineNumberRegex = regexp.MustCompile("\\.go:[\\d]+")

func scrubError(s string) string {
	s = scrubLineNumberAndPositionRegex.ReplaceAllString(s, ".go:x:y")
	return scrubLineNumberRegex.ReplaceAllString(s, ".go:x")
}

// loadTestCase reads a test case from a directory.
//
// The directory structure is:
//
//	root/
//
//		pkg
//			file containing the package name containing the inject function
//			(must also be package main)
//
//		...
//			any Go files found recursively placed under GOPATH/src/...
//
//		want/
//
//			wire_errs.txt
//					Expected errors from the Wire Generate function,
//					missing if no errors expected.
//					Distinct errors are separated by a blank line,
//					and line numbers and line positions are scrubbed
//					(e.g., "foo.go:52:8" --> "foo.go:x:y").
//
//			wire_gen.go
//					verified output of wire from a test run with
//					-record, missing if wire_errs.txt is present
//
//			program_out.txt
//					expected output from the final compiled program,
//					missing if wire_errs.txt is present
//
func loadTestCase(root string, wireGoSrc []byte) (*testCase, error) {
	name := filepath.Base(root)
	pkg, err := ioutil.ReadFile(filepath.Join(root, "pkg"))
	if err != nil {
		return nil, fmt.Errorf("load test case %s: %v", name, err)
	}
	var wantProgramOutput []byte
	var wantWireOutput []byte
	wireErrb, err := ioutil.ReadFile(filepath.Join(root, "want", "wire_errs.txt"))
	wantWireError := err == nil
	var wantWireErrorStrings []string
	if wantWireError {
		wantWireErrorStrings = strings.Split(scrubError(string(wireErrb)), "\n\n")
	} else {
		if !*setup.Record {
			wantWireOutput, err = ioutil.ReadFile(filepath.Join(root, "want", "wire_gen.go"))
			if err != nil {
				return nil, fmt.Errorf("load test case %s: %v, if this is a new testcase, run with -record to generate the wire_gen.go file", name, err)
			}
		}
		wantProgramOutput, err = ioutil.ReadFile(filepath.Join(root, "want", "program_out.txt"))
		if err != nil {
			return nil, fmt.Errorf("load test case %s: %v", name, err)
		}
	}
	goFiles := map[string][]byte{
		"github.com/google/go-cloud/wire/wire.go": wireGoSrc,
	}
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
		goFiles[filepath.Join("example.com", rel)] = data
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load test case %s: %v", name, err)
	}
	return &testCase{
		name:                 name,
		pkg:                  string(bytes.TrimSpace(pkg)),
		goFiles:              goFiles,
		wantWireOutput:       wantWireOutput,
		wantProgramOutput:    wantProgramOutput,
		wantWireError:        wantWireError,
		wantWireErrorStrings: wantWireErrorStrings,
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
	magicGOPATHUnix    = "/wire_gopath"
	magicGOPATHWindows = `C:\wire_gopath`
)

func magicGOPATH() string {
	if runtime.GOOS == "windows" {
		return magicGOPATHWindows
	}

	return magicGOPATHUnix
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
func (d dirInfo) Mode() os.FileMode  { return os.ModeDir | os.ModePerm }
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

// writeGoMod generates go.mod files for the test package and its dependency.
// The file structure looks like:
//
//	gopath/src/
//
//		example.com/
//
//			go.mod
//				replaces dependency with local copied one
//
//			... (Packages to be built and tested)
//				any Go files copied recursively
//
//		github.com/google/go-cloud/
//
//			go.mod
//
//			... (Dependency files copied)
func writeGoMod(gopath string) error {
	importPath := "example.com"
	depPath := "github.com/google/go-cloud"
	depLoc := filepath.Join(gopath, "src", filepath.FromSlash(depPath))
	example := fmt.Sprintf("module %s\n\nreplace %s => %s\n", importPath, depPath, depLoc)
	gomod := filepath.Join(gopath, "src", importPath, "go.mod")
	if err := ioutil.WriteFile(gomod, []byte(example), 0666); err != nil {
		return fmt.Errorf("generate go.mod for %s: %v", gomod, err)
	}
	if err := ioutil.WriteFile(filepath.Join(depLoc, "go.mod"), []byte("module "+depPath), 0666); err != nil {
		return fmt.Errorf("generate go.mod for %s: %v", depPath, err)
	}
	return nil
}

// runGo runs a go command in dir.
func runGo(bctx *build.Context, dir string, args ...string) error {
	exe := filepath.Join(bctx.GOROOT, "bin", "go")
	c := exec.Command(exe, args...)
	c.Env = append(os.Environ(), "GOROOT="+bctx.GOROOT, "GOARCH="+bctx.GOARCH, "GOOS="+bctx.GOOS, "GOPATH="+bctx.GOPATH)
	c.Dir = dir
	if bctx.CgoEnabled {
		c.Env = append(c.Env, "CGO_ENABLED=1")
	} else {
		c.Env = append(c.Env, "CGO_ENABLED=0")
	}
	// TODO(someday): Set -compiler flag if needed.
	out, err := c.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return fmt.Errorf("%v; output:\n%s", err, out)
		}
		return err
	}
	return nil
}

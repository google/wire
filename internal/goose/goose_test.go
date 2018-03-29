package goose

import (
	"bytes"
	"fmt"
	"go/build"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TODO(light): pull this out into a testdata directory

var tests = []struct {
	name       string
	files      map[string]string
	pkg        string
	wantOutput string
	wantError  bool
}{
	{
		name: "No-op build",
		files: map[string]string{
			"foo/foo.go": `package main; import "fmt"; func main() { fmt.Println("Hello, World!"); }`,
		},
		pkg:        "foo",
		wantOutput: "Hello, World!\n",
	},
	{
		name: "Niladic identity provider",
		files: map[string]string{
			"foo/foo.go": `package main
import "fmt"
func main() { fmt.Println(injectedMessage()); }

//goose:provide

// provideMessage provides a friendly user greeting.
func provideMessage() string { return "Hello, World!"; }
`,
			"foo/foo_goose.go": `//+build gooseinject

package main

//goose:use provideMessage

func injectedMessage() string
`,
		},
		pkg:        "foo",
		wantOutput: "Hello, World!\n",
	},
	{
		name: "Missing use",
		files: map[string]string{
			"foo/foo.go": `package main
import "fmt"
func main() { fmt.Println(injectedMessage()); }

//goose:provide Set

// provideMessage provides a friendly user greeting.
func provideMessage() string { return "Hello, World!"; }
`,
			"foo/foo_goose.go": `//+build gooseinject

package main

func injectedMessage() string
`,
		},
		pkg:       "foo",
		wantError: true,
	},
	{
		name: "Chain",
		files: map[string]string{
			"foo/foo.go": `package main
import "fmt"
func main() {
	fmt.Println(injectFooBar())
}

type Foo int
type FooBar int

//goose:provide Set
func provideFoo() Foo { return 41 }

//goose:provide Set
func provideFooBar(foo Foo) FooBar { return FooBar(foo) + 1 }
`,
			"foo/foo_goose.go": `//+build gooseinject

package main

//goose:use Set

func injectFooBar() FooBar
`,
		},
		pkg:        "foo",
		wantOutput: "42\n",
	},
	{
		name: "Two deps",
		files: map[string]string{
			"foo/foo.go": `package main
import "fmt"
func main() {
	fmt.Println(injectFooBar())
}

type Foo int
type Bar int
type FooBar int

//goose:provide Set
func provideFoo() Foo { return 40 }

//goose:provide Set
func provideBar() Bar { return 2 }

//goose:provide Set
func provideFooBar(foo Foo, bar Bar) FooBar { return FooBar(foo) + FooBar(bar) }
`,
			"foo/foo_goose.go": `//+build gooseinject

package main

//goose:use Set

func injectFooBar() FooBar
`,
		},
		pkg:        "foo",
		wantOutput: "42\n",
	},
	{
		name: "Inject input",
		files: map[string]string{
			"foo/foo.go": `package main
import "fmt"
func main() {
	fmt.Println(injectFooBar(40))
}

type Foo int
type Bar int
type FooBar int

//goose:provide Set
func provideBar() Bar { return 2 }

//goose:provide Set
func provideFooBar(foo Foo, bar Bar) FooBar { return FooBar(foo) + FooBar(bar) }
`,
			"foo/foo_goose.go": `//+build gooseinject

package main

//goose:use Set

func injectFooBar(foo Foo) FooBar
`,
		},
		pkg:        "foo",
		wantOutput: "42\n",
	},
	{
		name: "Inject input conflict",
		files: map[string]string{
			"foo/foo.go": `package main
import "fmt"
func main() {
	fmt.Println(injectBar(40))
}

type Foo int
type Bar int

//goose:provide Set
func provideFoo() Foo { return -888 }

//goose:provide Set
func provideBar(foo Foo) Bar { return 2 }
`,
			"foo/foo_goose.go": `//+build gooseinject

package main

//goose:use Set

func injectBar(foo Foo) Bar
`,
		},
		pkg:       "foo",
		wantError: true,
	},
	{
		name: "Return error",
		files: map[string]string{
			"foo/foo.go": `package main
import "errors"
import "fmt"
import "strings"
func main() {
	foo, err := injectFoo()
	fmt.Println(foo)
	if err == nil {
		fmt.Println("<nil>")
	} else {
		fmt.Println(strings.Contains(err.Error(), "there is no Foo"))
	}
}

type Foo int

//goose:provide Set
func provideFoo() (Foo, error) { return 42, errors.New("there is no Foo") }
`,
			"foo/foo_goose.go": `//+build gooseinject

package main

//goose:use Set

func injectFoo() (Foo, error)
`,
		},
		pkg:        "foo",
		wantOutput: "0\ntrue\n",
	},
}

func TestGeneratedCode(t *testing.T) {
	if _, err := os.Stat(filepath.Join(build.Default.GOROOT, "bin", "go")); err != nil {
		t.Fatalf("go toolchain not available: %v", err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gopath, err := ioutil.TempDir("", "goose_test")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(gopath)
			bctx := &build.Context{
				GOARCH:      build.Default.GOARCH,
				GOOS:        build.Default.GOOS,
				GOROOT:      build.Default.GOROOT,
				GOPATH:      gopath,
				CgoEnabled:  build.Default.CgoEnabled,
				Compiler:    build.Default.Compiler,
				ReleaseTags: build.Default.ReleaseTags,
			}
			for name, content := range test.files {
				p := filepath.Join(gopath, "src", filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(p), 0777); err != nil {
					t.Fatal(err)
				}
				if err := ioutil.WriteFile(p, []byte(content), 0666); err != nil {
					t.Fatal(err)
				}
			}
			gen, err := Generate(bctx, gopath, test.pkg)
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
			if len(gen) > 0 {
				genPath := filepath.Join(gopath, "src", filepath.FromSlash(test.pkg), "goose_gen.go")
				if err := ioutil.WriteFile(genPath, gen, 0666); err != nil {
					t.Fatal(err)
				}
			}
			testExePath := filepath.Join(gopath, "bin", "testprog")
			if err := runGo(bctx, "build", "-o", testExePath, test.pkg); err != nil {
				t.Fatal("build:", err)
			}
			out, err := exec.Command(testExePath).Output()
			if err != nil {
				t.Fatal("run compiled program:", err)
			}
			if string(out) != test.wantOutput {
				t.Errorf("compiled program output = %q; want %q", out, test.wantOutput)
			}
		})
	}
}

func TestDeterminism(t *testing.T) {
	runs := 10
	if testing.Short() {
		runs = 3
	}
	for _, test := range tests {
		if test.wantError {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			gopath, err := ioutil.TempDir("", "goose_test")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(gopath)
			bctx := &build.Context{
				GOARCH:      build.Default.GOARCH,
				GOOS:        build.Default.GOOS,
				GOROOT:      build.Default.GOROOT,
				GOPATH:      gopath,
				CgoEnabled:  build.Default.CgoEnabled,
				Compiler:    build.Default.Compiler,
				ReleaseTags: build.Default.ReleaseTags,
			}
			for name, content := range test.files {
				p := filepath.Join(gopath, "src", filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(p), 0777); err != nil {
					t.Fatal(err)
				}
				if err := ioutil.WriteFile(p, []byte(content), 0666); err != nil {
					t.Fatal(err)
				}
			}
			gold, err := Generate(bctx, gopath, test.pkg)
			if err != nil {
				t.Fatal("goose:", err)
			}
			goldstr := string(gold)
			for i := 0; i < runs-1; i++ {
				out, err := Generate(bctx, gopath, test.pkg)
				if err != nil {
					t.Fatal("goose (on subsequent run):", err)
				}
				if !bytes.Equal(gold, out) {
					t.Fatalf("goose output differs when run repeatedly on same input:\n%s", diff(goldstr, string(out)))
				}
			}
		})
	}
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

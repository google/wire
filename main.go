// goose is a compile-time dependency injection tool.
//
// See README.md for an overview.
package main

import (
	"fmt"
	"go/build"
	"io/ioutil"
	"os"
	"path/filepath"

	"codename/goose/internal/goose"
)

func main() {
	var pkg string
	switch len(os.Args) {
	case 1:
		pkg = "."
	case 2:
		pkg = os.Args[1]
	default:
		fmt.Fprintln(os.Stderr, "goose: usage: goose [PKG]")
		os.Exit(64)
	}
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "goose:", err)
		os.Exit(1)
	}
	pkgInfo, err := build.Default.Import(pkg, wd, build.FindOnly)
	if err != nil {
		fmt.Fprintln(os.Stderr, "goose:", err)
		os.Exit(1)
	}
	out, err := goose.Generate(&build.Default, wd, pkg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "goose:", err)
		os.Exit(1)
	}
	if len(out) == 0 {
		// No Goose directives, don't write anything.
		fmt.Fprintln(os.Stderr, "goose: no injector found for", pkg)
		return
	}
	p := filepath.Join(pkgInfo.Dir, "goose_gen.go")
	if err := ioutil.WriteFile(p, out, 0666); err != nil {
		fmt.Fprintln(os.Stderr, "goose:", err)
		os.Exit(1)
	}
}

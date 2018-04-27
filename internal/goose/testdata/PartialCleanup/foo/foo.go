package main

import (
	"errors"
	"fmt"
	"strings"
)

var (
	cleanedFoo = false
	cleanedBar = false
)

func main() {
	_, cleanup, err := injectBaz()
	if err == nil {
		fmt.Println("<nil>")
	} else {
		fmt.Println(strings.Contains(err.Error(), "bork!"))
	}
	fmt.Println(cleanedFoo, cleanedBar, cleanup == nil)
}

type Foo int
type Bar int
type Baz int

func provideFoo() (*Foo, func()) {
	foo := new(Foo)
	*foo = 42
	return foo, func() { *foo = 0; cleanedFoo = true }
}

func provideBar(foo *Foo) (*Bar, func(), error) {
	bar := new(Bar)
	*bar = 77
	return bar, func() {
		if *foo == 0 {
			panic("foo cleaned up before bar")
		}
		*bar = 0
		cleanedBar = true
	}, nil
}

func provideBaz(bar *Bar) (Baz, error) {
	return 0, errors.New("bork!")
}

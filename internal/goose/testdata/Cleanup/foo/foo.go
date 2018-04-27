package main

import (
	"fmt"
)

func main() {
	bar, cleanup := injectBar()
	fmt.Println(*bar)
	cleanup()
	fmt.Println(*bar)
}

type Foo int
type Bar int

func provideFoo() (*Foo, func()) {
	foo := new(Foo)
	*foo = 42
	return foo, func() { *foo = 0 }
}

func provideBar(foo *Foo) (*Bar, func()) {
	bar := new(Bar)
	*bar = 77
	return bar, func() {
		if *foo == 0 {
			panic("foo cleaned up before bar")
		}
		*bar = 0
	}
}

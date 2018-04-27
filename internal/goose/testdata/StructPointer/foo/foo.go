package main

import (
	"fmt"

	"codename/goose"
)

func main() {
	fb := injectFooBar()
	fmt.Println(fb.Foo, fb.Bar)
}

type Foo int
type Bar int

type FooBar struct {
	Foo Foo
	Bar Bar
}

func provideFoo() Foo {
	return 41
}

func provideBar() Bar {
	return 1
}

var Set = goose.NewSet(
	FooBar{},
	provideFoo,
	provideBar)

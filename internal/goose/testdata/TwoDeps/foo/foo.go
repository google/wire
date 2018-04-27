package main

import (
	"fmt"

	"codename/goose"
)

func main() {
	fmt.Println(injectFooBar())
}

type Foo int
type Bar int
type FooBar int

func provideFoo() Foo {
	return 40
}

func provideBar() Bar {
	return 2
}

func provideFooBar(foo Foo, bar Bar) FooBar {
	return FooBar(foo) + FooBar(bar)
}

var Set = goose.NewSet(
	provideFoo,
	provideBar,
	provideFooBar)

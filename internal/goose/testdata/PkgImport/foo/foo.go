package main

import (
	"fmt"

	"bar"
	"codename/goose"
)

func main() {
	fmt.Println(injectFooBar())
}

type Foo int
type FooBar int

var Set = goose.NewSet(
	provideFoo,
	bar.ProvideBar,
	provideFooBar)

func provideFoo() Foo {
	return 41
}

func provideFooBar(foo Foo, barVal bar.Bar) FooBar {
	return FooBar(foo) + FooBar(barVal)
}

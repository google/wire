package main

import (
	"fmt"

	"codename/goose"
)

func main() {
	fmt.Println(injectFooBar())
}

type Foo int
type FooBar int

var Set = goose.NewSet(
	provideFoo,
	provideFooBar)

func provideFoo() Foo {
	return 41
}

func provideFooBar(foo Foo) FooBar {
	return FooBar(foo) + 1
}

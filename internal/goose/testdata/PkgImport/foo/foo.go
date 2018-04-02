package main

import (
	"fmt"

	"bar"
)

func main() {
	fmt.Println(injectFooBar())
}

type Foo int
type FooBar int

//goose:provide Set
func provideFoo() Foo {
	return 41
}

//goose:import Set "bar".Bar

//goose:provide Set
func provideFooBar(foo Foo, barVal bar.Bar) FooBar {
	return FooBar(foo) + FooBar(barVal)
}

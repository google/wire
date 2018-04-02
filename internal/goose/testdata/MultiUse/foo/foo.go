package main

import "fmt"

func main() {
	fmt.Println(injectFooBar())
}

type Foo int
type FooBar int

//goose:provide Foo
func provideFoo() Foo {
	return 41
}

//goose:provide FooBar
func provideFooBar(foo Foo) FooBar {
	return FooBar(foo) + 1
}

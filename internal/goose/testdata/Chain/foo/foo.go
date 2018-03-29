package main

import "fmt"

func main() {
	fmt.Println(injectFooBar())
}

type Foo int
type FooBar int

//goose:provide Set
func provideFoo() Foo {
	return 41
}

//goose:provide Set
func provideFooBar(foo Foo) FooBar {
	return FooBar(foo) + 1
}

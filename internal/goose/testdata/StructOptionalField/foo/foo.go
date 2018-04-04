package main

import "fmt"

func main() {
	fb := injectFooBar()
	fmt.Println(fb.Foo, fb.OptionalBar)
}

type Foo int
type Bar int

//goose:provide Set
//goose:optional OptionalBar
type FooBar struct {
	Foo         Foo
	OptionalBar Bar
}

//goose:provide Set
func provideFoo() Foo {
	return 42
}

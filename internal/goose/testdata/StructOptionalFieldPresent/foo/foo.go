package main

import "fmt"

func main() {
	fb := injectFooBar()
	fmt.Println(fb.Foo, fb.Bar)
}

type Foo int
type Bar int

//goose:provide Set
//goose:optional Bar
type FooBar struct {
	Foo Foo
	Bar Bar
}

//goose:provide Set
func provideFoo() Foo {
	return 41
}

//goose:provide Set
func provideBar() Bar {
	return 1
}

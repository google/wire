package main

import "fmt"

func main() {
	fmt.Println(injectFooBar())
}

type Foo int
type Bar int
type FooBar int

//goose:provide Set
func provideFoo() Foo {
	return 40
}

//goose:provide Set
func provideBar() Bar {
	return 2
}

//goose:provide Set
func provideFooBar(foo Foo, bar Bar) FooBar {
	return FooBar(foo) + FooBar(bar)
}

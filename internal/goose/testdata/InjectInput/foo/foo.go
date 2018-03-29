package main

import "fmt"

func main() {
	fmt.Println(injectFooBar(40))
}

type Foo int
type Bar int
type FooBar int

//goose:provide Set
func provideBar() Bar {
	return 2
}

//goose:provide Set
func provideFooBar(foo Foo, bar Bar) FooBar {
	return FooBar(foo) + FooBar(bar)
}

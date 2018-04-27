package main

import (
	"fmt"

	"codename/goose"
)

func main() {
	fmt.Println(injectFooBar(40))
}

type Foo int
type Bar int
type FooBar int

var Set = goose.NewSet(
	provideBar,
	provideFooBar)

func provideBar() Bar {
	return 2
}

func provideFooBar(foo Foo, bar Bar) FooBar {
	return FooBar(foo) + FooBar(bar)
}

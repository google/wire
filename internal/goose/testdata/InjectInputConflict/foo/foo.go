package main

import (
	"fmt"

	"codename/goose"
)

func main() {
	// I'm on the fence as to whether this should be an error (versus an
	// override). For now, I will make it an error that can be relaxed
	// later.
	fmt.Println(injectBar(40))
}

type Foo int
type Bar int

var Set = goose.NewSet(
	provideFoo,
	provideBar)

func provideFoo() Foo {
	return -888
}

func provideBar(foo Foo) Bar {
	return 2
}

package main

import "fmt"

func main() {
	// I'm on the fence as to whether this should be an error (versus an
	// override). For now, I will make it an error that can be relaxed
	// later.
	fmt.Println(injectBar(40))
}

type Foo int
type Bar int

//goose:provide Set
func provideFoo() Foo {
	return -888
}

//goose:provide Set
func provideBar(foo Foo) Bar {
	return 2
}

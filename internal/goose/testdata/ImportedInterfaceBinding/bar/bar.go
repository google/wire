package main

import (
	"fmt"

	_ "foo"
)

func main() {
	fmt.Println(injectFooer().Foo())
}

type Bar string

func (b *Bar) Foo() string {
	return string(*b)
}

//goose:provide
func provideBar() *Bar {
	b := new(Bar)
	*b = "Hello, World!"
	return b
}

//goose:bind provideBar "foo".Fooer *Bar

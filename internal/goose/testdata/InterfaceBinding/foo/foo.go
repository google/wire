package main

import (
	"fmt"

	"codename/goose"
)

func main() {
	fmt.Println(injectFooer().Foo())
}

type Fooer interface {
	Foo() string
}

type Bar string

func (b *Bar) Foo() string {
	return string(*b)
}

func provideBar() *Bar {
	b := new(Bar)
	*b = "Hello, World!"
	return b
}

var Set = goose.NewSet(
	provideBar,
	goose.Bind(Fooer(nil), (*Bar)(nil)))

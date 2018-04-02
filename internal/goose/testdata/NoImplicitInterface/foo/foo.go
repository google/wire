package main

import "fmt"

func main() {
	fmt.Println(injectFooer().Foo())
}

type Fooer interface {
	Foo() string
}

type Bar string

func (b Bar) Foo() string {
	return string(b)
}

//goose:provide
func provideBar() Bar {
	return "Hello, World!"
}

//+build gooseinject

package main

import (
	"codename/goose"
)

func injectFooBar(foo Foo) FooBar {
	panic(goose.Use(Set))
}

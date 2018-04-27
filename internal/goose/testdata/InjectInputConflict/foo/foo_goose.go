//+build gooseinject

package main

import (
	"codename/goose"
)

func injectBar(foo Foo) Bar {
	panic(goose.Use(Set))
}

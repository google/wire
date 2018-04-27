//+build gooseinject

package main

import (
	"codename/goose"
	"foo"
)

func injectFooer() foo.Fooer {
	panic(goose.Use(Set))
}

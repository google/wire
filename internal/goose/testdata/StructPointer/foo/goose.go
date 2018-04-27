//+build gooseinject

package main

import (
	"codename/goose"
)

func injectFooBar() *FooBar {
	panic(goose.Use(Set))
}

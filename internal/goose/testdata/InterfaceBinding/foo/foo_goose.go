//+build gooseinject

package main

import (
	"codename/goose"
)

func injectFooer() Fooer {
	panic(goose.Use(Set))
}

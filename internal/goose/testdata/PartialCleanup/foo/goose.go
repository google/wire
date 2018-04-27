//+build gooseinject

package main

import (
	"codename/goose"
)

func injectBaz() (Baz, func(), error) {
	panic(goose.Use(provideFoo, provideBar, provideBaz))
}

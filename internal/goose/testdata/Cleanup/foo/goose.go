//+build gooseinject

package main

import (
	"codename/goose"
)

func injectBar() (*Bar, func()) {
	panic(goose.Use(provideFoo, provideBar))
}

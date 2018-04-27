//+build gooseinject

package main

import (
	"codename/goose"
)

func injectFooBar() FooBar {
	panic(goose.Use(
		provideBar,
		provideFooBar,
		goose.Bind(Fooer(nil), (*Bar)(nil))))
}

//+build gooseinject

package main

import "foo"

//goose:use provideBar

func injectFooer() foo.Fooer

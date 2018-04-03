//+build gooseinject

package main

//goose:use Foo
//goose:use Bar
//goose:use Baz

func injectBaz() (Baz, func(), error)

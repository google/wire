//+build gooseinject

package main

//goose:use Foo
//goose:use Bar

func injectBar() (*Bar, func())

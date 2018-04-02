//+build gooseinject

package main

//goose:use Foo FooBar

func injectFooBar() FooBar

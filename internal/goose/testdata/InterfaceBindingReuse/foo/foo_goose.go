//+build gooseinject

package main

//goose:use provideBar
//goose:use provideFooBar

func injectFooBar() FooBar

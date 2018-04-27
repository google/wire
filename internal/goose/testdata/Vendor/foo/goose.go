//+build gooseinject

package main

import (
	"bar"
	"codename/goose"
)

func injectedMessage() string {
	panic(goose.Use(bar.ProvideMessage))
}

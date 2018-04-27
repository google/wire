//+build gooseinject

package main

import (
	"codename/goose"
)

func injectedMessage() string {
	panic(goose.Use(provideMessage))
}

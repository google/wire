//+build gooseinject

package main

import (
	_ "bar"
)

//goose:use "bar".Message

func injectedMessage() string

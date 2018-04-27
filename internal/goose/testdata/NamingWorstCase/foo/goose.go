//+build gooseinject

package main

import (
	stdcontext "context"

	"codename/goose"
)

func inject(context stdcontext.Context, err struct{}) (context, error) {
	panic(goose.Use(provide))
}

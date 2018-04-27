//+build gooseinject

package main

import (
	stdcontext "context"

	"codename/goose"
)

// The notable characteristic of this test is that there are no
// parameter names on the inject stub.

func inject(stdcontext.Context, struct{}) (context, error) {
	panic(goose.Use(provide))
}

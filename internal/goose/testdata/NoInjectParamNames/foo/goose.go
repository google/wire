//+build gooseinject

package main

import (
	stdcontext "context"
)

// The notable characteristic of this test is that there are no
// parameter names on the inject stub.

//goose:use provide

func inject(stdcontext.Context, struct{}) (context, error)

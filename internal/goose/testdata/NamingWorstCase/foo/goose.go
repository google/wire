//+build gooseinject

package main

import (
	stdcontext "context"
)

//goose:use provide

func inject(context stdcontext.Context, err struct{}) (context, error)

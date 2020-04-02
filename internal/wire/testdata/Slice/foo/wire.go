//+build wireinject

package main

import (
	"github.com/google/wire"
)

func NewRouter() *Mux {
	panic(
		wire.Build(
			InitRouter,
			wire.Slice(
				[]Controller(nil),
				NewHome,
				NewUpload,
			),
		),
	)
}


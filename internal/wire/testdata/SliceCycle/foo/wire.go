//+build wireinject

package main

import (
	"github.com/google/wire"
)

func NewRouter() *Mux {
	panic(
		wire.Build(
			InitRouter,
			NewRouterService,
			wire.Slice(
				[]Controller(nil),
				NewHome,
				NewUpload,
			),
		),
	)
}

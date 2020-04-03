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
				wire.Struct(new(HomeController), "*"),
				wire.Value(&UploadController{}),
			),
		),
	)
}


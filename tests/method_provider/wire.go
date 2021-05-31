//+build wireinject
//go:generate wire

package method_provider

import (
	"github.com/google/wire"
	"github.com/google/wire/tests/method_provider/animals"
)

func InitDog() *animals.Dog {
	panic(wire.Build(
		animals.NewAnimals,
		(*animals.Animals).NewDog,
	))
}

func InitCat() *animals.Cat {
	panic(wire.Build(
		wire.Value(animals.Animals{}),
		animals.Animals.NewCat,
	))
}

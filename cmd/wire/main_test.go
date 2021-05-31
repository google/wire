package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMethodProvider(t *testing.T) {
	/*
		// Support type's method as provider, for example:

		func InitDog() *animals.Dog {
			panic(wire.Build(
				animals.NewAnimals,
				(*animals.Animals).NewDog, // pointer receiver method
			))
		}

		func InitCat() *animals.Cat {
			panic(wire.Build(
				wire.Value(animals.Animals{}),
				animals.Animals.NewCat, // struct receiver method
			))
		}
	*/
	_, b, _, _ := runtime.Caller(0)
	wireRepoPath := filepath.Dir(filepath.Dir(filepath.Dir(b)))
	_ = os.Chdir(filepath.Join(wireRepoPath, "tests", "method_provider"))
	cmd := &genCmd{}
	code := int(cmd.Execute(context.Background(), flag.CommandLine))
	if code != 0 {
		t.Fatal(code)
	}
}

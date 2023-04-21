// Copyright 2018 The Wire Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build wireinject
// +build wireinject

package main

import (
	// "strings"

	"github.com/google/wire"
)

func inject(name BarName, opts *FooOptions) *Bar {
	panic(wire.Build(wire.Subtract(Set, new(FooOptions))))
}

func injectBarService(name BarName, opts *FakeBarService) *FooBar {
	panic(wire.Build(
		wire.Subtract(SuperSet, new(BarService)),
		wire.Bind(new(BarService), new(*FakeBarService)),
	))
}

func injectFooBarService(name BarName, opts *FooOptions, bar *FakeBarService) *FooBar {
	panic(wire.Build(
		wire.Subtract(SuperSet, new(FooOptions), new(BarService)),
		wire.Bind(new(BarService), new(*FakeBarService)),
	))
}

func injectNone(name BarName, foo Foo, bar *FakeBarService) *FooBar {
	panic(wire.Build(
		wire.Subtract(SuperSet, new(Foo), new(BarService)),
		wire.Bind(new(BarService), new(*FakeBarService)),
	))
}

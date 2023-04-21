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

package main

import (
	"github.com/google/wire"
)

type context struct{}

func main() {}

type FooOptions struct{}
type Foo string
type Bar struct{}
type BarName string

func (b *Bar) Bar() {}

func provideFooOptions() *FooOptions {
	return &FooOptions{}
}

func provideFoo(*FooOptions) Foo {
	return Foo("foo")
}

func provideBar(Foo, BarName) *Bar {
	return &Bar{}
}

type BarService interface {
	Bar()
}

type FooBar struct {
	BarService
	Foo
}

var Set = wire.NewSet(
	provideFooOptions,
	provideFoo,
	provideBar,
)

var SuperSet = wire.NewSet(Set,
	wire.Struct(new(FooBar), "*"),
	wire.Bind(new(BarService), new(*Bar)),
)

type FakeBarService struct{}

func (f *FakeBarService) Bar() {}

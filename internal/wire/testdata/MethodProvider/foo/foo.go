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
	"fmt"

	"github.com/google/wire"
)

func main() {
	fmt.Println(injectFooBar())
}

type (
	Foo    int
	FooBar int
	Baz    int
)

var Set = wire.NewSet(
	newFooProvider,
	fooProvider.provideFoo,
	newFooBarProvider,
	(*fooBarProvider).provideFooBar,
	newBazProvider,
	bazProvider.provideBaz,
)

type fooProvider struct{}

func newFooProvider() fooProvider {
	return fooProvider{}
}

func (f fooProvider) provideFoo() Foo {
	return 40
}

type fooBarProvider struct{}

func newFooBarProvider() *fooBarProvider {
	return &fooBarProvider{}
}

func (*fooBarProvider) provideFooBar(foo Foo) FooBar {
	return FooBar(foo) + 1
}

type bazProvider interface {
	provideBaz(f FooBar) Baz
}

func newBazProvider() bazProvider {
	return bazProviderImpl{}
}

type bazProviderImpl struct{}

func (bazProviderImpl) provideBaz(fooBar FooBar) Baz {
	return Baz(fooBar) + 1
}

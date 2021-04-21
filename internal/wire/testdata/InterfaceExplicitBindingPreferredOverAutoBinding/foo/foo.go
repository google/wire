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
	fmt.Println(injectPlugher().Plugh())
}

type Fooer interface {
	Foo() string
}

type Plugher interface {
	Plugh() string
}

type Bar string

func (b *Bar) Foo() string {
	return string(*b)
}

func provideBar() *Bar {
	b := new(Bar)
	*b = "Bar!"
	return b
}

type Qux string

func (q *Qux) Foo() string {
	return string(*q)
}

func (q *Qux) Plugh() string {
	return string(*q) + string(*q)
}

func provideQux(fooer Fooer) *Qux {
	b := new(Qux)
	*b = Qux("Qux!" + fooer.Foo())
	return b
}

var Set = wire.NewSet(provideBar, provideQux)

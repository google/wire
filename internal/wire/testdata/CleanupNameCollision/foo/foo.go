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
)

func main() {
	bar, cleanup := injectFooCleanup()
	fmt.Println(*bar)
	cleanup()
	fmt.Println(*bar)
}

type Foo int
type FooCleanup int

func provideFoo() (*Foo, func()) {
	foo := new(Foo)
	*foo = 42
	return foo, func() { *foo = 0 }
}

func provideFooCleanup(foo *Foo) (*FooCleanup, func()) {
	fooCleanup := new(FooCleanup)
	*fooCleanup = 77
	return fooCleanup, func() {
		if *foo == 0 {
			panic("foo cleaned up before foo cleanup")
		}
		*fooCleanup = 0
	}
}

// Copyright 2018 The Wire Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package main

type Foo string

type FooStorer interface {
	Set(key string, value Foo)
	Get(key string) (Foo, bool)
}

type Store[TKey comparable, TValue any] struct {
	store map[TKey]TValue
}

func NewStore[TKey comparable, TValue any]() Store[TKey, TValue] {
	return Store[TKey, TValue]{store: make(map[TKey]TValue)}
}

func (s Store[TKey, TValue]) Set(key TKey, value TValue) {
	s.store[key] = value
}

func (s Store[TKey, TValue]) Get(key TKey) (TValue, bool) {
	value, ok := s.store[key]
	return value, ok
}

func NewFooStore(foo Foo) Store[string, Foo] {
	r := NewStore[string, Foo]()
	r.Set("foo", foo)
	return r
}

func main() {
	fooStore := InitializeFooStore()
	v, ok := fooStore.Get("foo")
	if !ok {
		panic("foo not found")
	}
	print(v)

}

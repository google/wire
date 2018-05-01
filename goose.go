// Copyright 2018 Google LLC
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

// Package goose contains directives for goose code generation.
package goose

// ProviderSet is a marker type that collects a group of providers.
type ProviderSet struct{}

// NewSet creates a new provider set that includes the providers in
// its arguments. Each argument is either an exported function value,
// an exported struct (zero) value, or a call to Bind.
func NewSet(...interface{}) ProviderSet {
	return ProviderSet{}
}

// Use is placed in the body of an injector function to declare the
// providers to use. Its arguments are the same as NewSet. Its return
// value is an error message that can be sent to panic.
//
// Example:
//
//	func injector(ctx context.Context) (*sql.DB, error) {
//		panic(Use(otherpkg.Foo, myProviderFunc, goose.Bind()))
//	}
func Use(...interface{}) string {
	return "implementation not generated, run goose"
}

// A Binding maps an interface to a concrete type.
type Binding struct{}

// Bind declares that a concrete type should be used to satisfy a
// dependency on iface.
//
// Example:
//
//	var MySet = goose.NewSet(goose.Bind(MyInterface(nil), new(MyStruct)))
func Bind(iface, to interface{}) Binding {
	return Binding{}
}

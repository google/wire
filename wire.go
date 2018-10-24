// Copyright 2018 The Go Cloud Authors
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

// Package wire contains directives for Wire code generation.
package wire

// ProviderSet is a marker type that collects a group of providers.
type ProviderSet struct{}

// NewSet creates a new provider set that includes the providers in
// its arguments. Each argument is either an exported function value,
// an exported struct (zero) value, a provider set, a call to Bind,
// a call to Value, or a call to InterfaceValue.
func NewSet(...interface{}) ProviderSet {
	return ProviderSet{}
}

// Build is placed in the body of an injector function to declare the
// providers to use. Its arguments are the same as NewSet. Its return
// value is an error message that can be sent to panic.
//
// Examples:
//
//	func injector(ctx context.Context) (*sql.DB, error) {
//		wire.Build(otherpkg.FooSet, myProviderFunc)
//		return nil, nil
//	}
//
//	func injector(ctx context.Context) (*sql.DB, error) {
//		panic(wire.Build(otherpkg.FooSet, myProviderFunc))
//	}
func Build(...interface{}) string {
	return "implementation not generated, run wire"
}

// A Binding maps an interface to a concrete type.
type Binding struct{}

// Bind declares that a concrete type should be used to satisfy a
// dependency on the type of iface, which must be a pointer to an
// interface type.
//
// Example:
//
//	type Fooer interface {
//		Foo()
//	}
//
//	type MyFoo struct{}
//
//	func (MyFoo) Foo() {}
//
//	var MySet = wire.NewSet(
//		MyFoo{},
//		wire.Bind(new(Fooer), new(MyFoo)))
func Bind(iface, to interface{}) Binding {
	return Binding{}
}

// A ProvidedValue is an expression that is copied to the generated injector.
type ProvidedValue struct{}

// Value binds an expression to provide the type of the expression.
// The expression may not be an interface value; use InterfaceValue for that.
//
// Example:
//
//	var MySet = wire.NewSet(wire.Value([]string(nil)))
func Value(interface{}) ProvidedValue {
	return ProvidedValue{}
}

// InterfaceValue binds an expression to provide a specific interface type.
//
// Example:
//
//	var MySet = wire.NewSet(wire.InterfaceValue(new(io.Reader), os.Stdin))
func InterfaceValue(typ interface{}, x interface{}) ProvidedValue {
	return ProvidedValue{}
}

// Code generated by Wire. DO NOT EDIT.

//go:generate go run github.com/google/wire/cmd/wire@latest
//go:build !wireinject
// +build !wireinject

package main

// Injectors from wire.go:

func injectedMessage(t title, lines ...string) string {
	string2 := provideMessage(lines...)
	return string2
}

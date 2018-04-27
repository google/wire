package main

import (
	"errors"
	"fmt"
	"strings"

	"codename/goose"
)

func main() {
	foo, err := injectFoo()
	fmt.Println(foo) // should be zero, the injector should ignore provideFoo's return value.
	if err == nil {
		fmt.Println("<nil>")
	} else {
		fmt.Println(strings.Contains(err.Error(), "there is no Foo"))
	}
}

type Foo int

func provideFoo() (Foo, error) {
	return 42, errors.New("there is no Foo")
}

var Set = goose.NewSet(provideFoo)

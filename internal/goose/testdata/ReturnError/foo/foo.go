package main

import "errors"
import "fmt"
import "strings"

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

//goose:provide Set
func provideFoo() (Foo, error) {
	return 42, errors.New("there is no Foo")
}

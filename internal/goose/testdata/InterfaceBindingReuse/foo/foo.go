// This test verifies that the concrete type is provided only once, even if an
// interface additionally depends on it.

package main

import (
	"fmt"
	"sync"
)

func main() {
	injectFooBar()
	fmt.Println(provideBarCalls)
}

type Fooer interface {
	Foo() string
}

type Bar string

type FooBar struct {
	Fooer Fooer
	Bar   *Bar
}

func (b *Bar) Foo() string {
	return string(*b)
}

//goose:provide
//goose:bind provideBar Fooer *Bar
func provideBar() *Bar {
	mu.Lock()
	provideBarCalls++
	mu.Unlock()
	b := new(Bar)
	*b = "Hello, World!"
	return b
}

var (
	mu              sync.Mutex
	provideBarCalls int
)

//goose:provide
func provideFooBar(fooer Fooer, bar *Bar) FooBar {
	return FooBar{fooer, bar}
}

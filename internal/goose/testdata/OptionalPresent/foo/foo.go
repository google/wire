package main

import "fmt"

func main() {
	fmt.Println(injectBar(42))
}

type foo int
type bar int

//goose:provide
//goose:optional f
func provideBar(f foo) bar {
	return bar(f)
}

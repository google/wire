package main

import "fmt"

func main() {
	fmt.Println(injectedMessage())
}

//goose:provide Set

// provideMessage provides a friendly user greeting.
func provideMessage() string {
	return "Hello, World!"
}

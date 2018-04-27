package main

import (
	"fmt"
)

func main() {
	fmt.Println(injectedMessage())
}

var myFakeSet struct{}

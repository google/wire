package main

import (
	stdcontext "context"
	"fmt"
	"os"
)

type context struct{}

func main() {
	c, err := inject(stdcontext.Background(), struct{}{})
	if err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(1)
	}
	fmt.Println(c)
}

//goose:provide

func provide(ctx stdcontext.Context) (context, error) {
	return context{}, nil
}

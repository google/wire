# async-wire: A fork of wire for asynchronous dependency graphs

async-wire extends [google/wire](https://github.com/google/wire) by introducing a new provider type called `wire.AsyncFunc`.
Functions wrapped by `AsyncFunc` are executed in a Goroutine. To synchronise inputs / outputs between providers, channels are used.

## Project goals

1. First class support for asynchronous dependency graphs
2. Compatibility with existing Wire features
3. .dot file generation (TODO)

## Extended API

```go
// wire.go

//go:build wireinject
// +build wireinject

type A int
type B int

func Sum(ctx context.Context) (int, error) {
	wire.Build(
		wire.AsyncFunc(slowA),
		wire.AsyncFunc(slowB),
		sum,
	)
	return 0, nil
}

func slowA() A {
  time.Sleep(1 * time.Second)
  return A(1) 
}

func slowB() B {
  time.Sleep(1 * time.Second)
  return B(1) 
}

func sum(a A, b B) int {
    return int(a) + int(b)
}

// wire_gen.go

func Sum() (int, error) {
    g, err := errgroup.WithContext(ctx)

    aChan := make(chan A, 1)
    g.Go(func() error {
       a := slowA()
       ...
    })

    bChan := make(chan B, 1)
    g.Go(func() error {
       b := slowB()
       ...
    })

    sumChan := make(chan int, 1)
    g.Go(func() error) {
       ...
       s := sum(a, b)
       ...
    }

    if err := g.Wait(); err != nil {
        return 0, err
    }

    return <-sumChan, nil
}

// app.go

t := time.Now()
s, err := Sum() 
fmt.Printf("Sum = %d after %d second", s, time.Since(t).Seconds())
// Sum = 2 after 1 second
```

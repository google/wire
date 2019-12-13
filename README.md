# Wire: Automated Initialization in Go

[![Build Status](https://travis-ci.com/google/wire.svg?branch=master)][travis]
[![godoc](https://godoc.org/github.com/google/wire?status.svg)][godoc]
[![Coverage](https://codecov.io/gh/google/wire/branch/master/graph/badge.svg)](https://codecov.io/gh/google/wire)


Wire is a code generation tool that automates connecting components using
[dependency injection][]. Dependencies between components are represented in
Wire as function parameters, encouraging explicit initialization instead of
global variables. Because Wire operates without runtime state or reflection,
code written to be used with Wire is useful even for hand-written
initialization.

For an overview, see the [introductory blog post][].

[dependency injection]: https://en.wikipedia.org/wiki/Dependency_injection
[introductory blog post]: https://blog.golang.org/wire
[godoc]: https://godoc.org/github.com/google/wire
[travis]: https://travis-ci.com/google/wire

## Installing

Install Wire by running:

```shell
go get github.com/google/wire/cmd/wire
```

and ensuring that `$GOPATH/bin` is added to your `$PATH`.

## Documentation

- [Tutorial][]
- [User Guide][]
- [Best Practices][]
- [FAQ][]

[Tutorial]: ./_tutorial/README.md
[Best Practices]: ./docs/best-practices.md
[FAQ]: ./docs/faq.md
[User Guide]: ./docs/guide.md

## Project status

As of version v0.3.0, Wire is *beta*. Throughout the beta period, we encourage
you to use Wire and provide feedback. We will focus on improving usability and
fixing bugs before the 1.0 release. We do not intend to introduce any breaking
changes unless there is a strong reason to do so.

## Community

You can contact us on the [go-cloud mailing list][].

This project is covered by the Go [Code of Conduct][].

[Code of Conduct]: ./CODE_OF_CONDUCT.md
[go-cloud mailing list]: https://groups.google.com/forum/#!forum/go-cloud

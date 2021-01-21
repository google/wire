ARG GO_VERSION=1.15

### Builder
FROM golang:${GO_VERSION} as builder
COPY . /go/src/github.com/google/wire
WORKDIR /go/src/github.com/google/wire
RUN GO111MODULE=on GOOS=linux go build -o ./bin/wire -v ./cmd/wire

### Image
FROM golang:${GO_VERSION}
COPY --from=builder /go/src/github.com/google/wire/bin/wire /go/bin/wire
RUN chmod +x /go/bin/wire
ENTRYPOINT ["/go/bin/wire"]

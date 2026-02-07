.PHONY: all fmt build test stress

all: fmt build test

fmt:
	gofmt -w $$(find . -name '*.go')

build:
	go build ./...

test:
	go test -count=1 ./...

stress:
	go run ./cmd/ace test

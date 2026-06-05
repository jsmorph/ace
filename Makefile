.PHONY: all fmt build test stress install clean

COMMIT := $(shell git rev-parse HEAD 2>/dev/null)
LDFLAGS := -X github.com/morphism/ace.Commit=$(COMMIT)

all: fmt build test

fmt:
	gofmt -w $$(find . -name '*.go')

build: ace

ace: $(wildcard *.go) $(wildcard cmd/ace/*.go) $(wildcard core/*.go) $(wildcard cli/*.go) $(wildcard netapi/*.go) $(wildcard mcp/*.go) $(wildcard docs/*.md)
	go build -ldflags '$(LDFLAGS)' -o ace ./cmd/ace

test:
	go test -count=1 ./...

stress: ace
	./ace test

install:
	go install -ldflags '$(LDFLAGS)' ./cmd/ace

clean:
	rm -f ace

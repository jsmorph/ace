.PHONY: all fmt build test stress install clean

all: fmt build test

fmt:
	gofmt -w $$(find . -name '*.go')

build: ace

ace: $(wildcard *.go) $(wildcard cmd/ace/*.go) $(wildcard *.md)
	go build -o ace ./cmd/ace

test:
	go test -count=1 ./...

stress: ace
	./ace test

install:
	cd cmd/ace && go install

clean:
	rm -f ace

BINARY ?= rift

.PHONY: build test lint fmt

build:
	go build -o $(BINARY) ./cmd/rift

test:
	go test ./...

lint:
	go vet ./...

fmt:
	gofmt -w $$(rg --files -g '*.go')

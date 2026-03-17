BINARY := bin/frieren
CONFIG ?= example.toml

.PHONY: build run fmt test clean

build:
	@mkdir -p bin
	go build -o $(BINARY) ./cmd/frieren

run:
	go run ./cmd/frieren -config $(CONFIG)

fmt:
	go fmt ./...

test:
	go test ./...

clean:
	rm -rf bin

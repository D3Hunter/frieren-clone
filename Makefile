BINARY_DIR := bin
BINARY_NAME := frieren
BINARY := $(BINARY_DIR)/$(BINARY_NAME)
LINUX_AMD64_BINARY := $(BINARY_DIR)/$(BINARY_NAME)-linux-amd64
CONFIG ?= example.toml

.PHONY: build build-linux-amd64 build-linux-x86_64 run fmt test clean

build:
	@mkdir -p $(BINARY_DIR)
	go build -o $(BINARY) ./cmd/frieren

build-linux-amd64:
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(LINUX_AMD64_BINARY) ./cmd/frieren

build-linux-x86_64: build-linux-amd64

run:
	go run ./cmd/frieren -config $(CONFIG)

fmt:
	go fmt ./...

test:
	go test ./...

clean:
	rm -rf bin

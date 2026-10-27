.PHONY: run test build fmt

run:
	go run ./cmd/server

test:
	go test ./...

build:
	go build -o bin/server ./cmd/server

fmt:
	go fmt ./...

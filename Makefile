.PHONY: run test build fmt

# Переменные из .env (если есть) уходят в окружение сервера.
-include .env
export

run:
	go run ./cmd/server

test:
	go test ./...

build:
	go build -o bin/server ./cmd/server

fmt:
	go fmt ./...

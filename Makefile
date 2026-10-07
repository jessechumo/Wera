.PHONY: build test lint migrate fetch score pipeline serve worker bench deep clean

# All configuration comes from the environment or a local .env file,
# which the binary loads itself. Nothing secret belongs in this Makefile.

build:
	go build -o bin/wera ./cmd/wera

test:
	go test ./...

lint:
	go vet ./...

migrate:
	go run ./cmd/wera migrate

fetch:
	go run ./cmd/wera fetch

score:
	go run ./cmd/wera score

pipeline:
	go run ./cmd/wera pipeline

serve:
	go run ./cmd/wera serve

worker:
	go run ./cmd/wera worker

bench:
	go run ./cmd/wera bench

deep:
	go run ./cmd/wera deep

clean:
	rm -rf bin

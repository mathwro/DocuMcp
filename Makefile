.PHONY: build test lint clean

build:
	CGO_ENABLED=1 go build -o bin/documcp ./cmd/documcp

test:
	CGO_ENABLED=1 go test ./... -v -timeout 60s

lint:
	golangci-lint run

clean:
	rm -rf bin/

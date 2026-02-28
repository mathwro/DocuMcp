.PHONY: build test lint docker run clean

build:
	CGO_ENABLED=1 go build -tags sqlite_fts5 -o bin/documcp ./cmd/documcp

test:
	CGO_ENABLED=1 go test -tags sqlite_fts5 ./... -v -timeout 60s

docker:
	docker build -t documcp:local .

run: docker
	docker-compose up

lint:
	golangci-lint run

clean:
	rm -rf bin/

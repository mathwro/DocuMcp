.PHONY: build bench bench-run test lint docker run clean init

# Detect OS and architecture from the Go toolchain
GOOS   := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)

# Binary name — appends .exe on Windows
BINARY_EXT :=
ifeq ($(GOOS),windows)
  BINARY_EXT := .exe
endif
BINARY := bin/documcp$(BINARY_EXT)
BENCH_BINARY := bin/bench$(BINARY_EXT)
BENCH_TASKS ?= docs/bench/codex-tasks.example.jsonl
BENCH_MODE ?= both
BENCH_RUNS ?= 1
BENCH_DOCUMCP_URL ?= http://localhost:8080/mcp/http
BENCH_OUT ?= bench-results.json
BENCH_RAW_DIR ?= bench-events

# Auto-detect container runtime: prefer podman if available, fall back to docker
# `which` works on Linux, macOS, WSL, and Git Bash on Windows
_PODMAN := $(shell which podman 2>/dev/null)
CONTAINER_RUNTIME := $(if $(_PODMAN),podman,docker)
_PODMAN_COMPOSE := $(shell which podman-compose 2>/dev/null)
COMPOSE_CMD := $(if $(_PODMAN_COMPOSE),podman-compose,$(CONTAINER_RUNTIME) compose)

build:
	CGO_ENABLED=1 go build -tags sqlite_fts5 -o $(BINARY) ./cmd/documcp

bench:
	CGO_ENABLED=1 go build -tags sqlite_fts5 -o $(BENCH_BINARY) ./cmd/bench

bench-run: bench
	$(BENCH_BINARY) -tasks $(BENCH_TASKS) -mode $(BENCH_MODE) -runs $(BENCH_RUNS) -documcp-url $(BENCH_DOCUMCP_URL) -out $(BENCH_OUT) -raw-dir $(BENCH_RAW_DIR)

test:
	CGO_ENABLED=1 go test -tags sqlite_fts5 ./... -v -timeout 60s

docker:
	$(CONTAINER_RUNTIME) build -t documcp:local .

run:
	$(COMPOSE_CMD) up

lint:
	golangci-lint run

clean:
ifeq ($(GOOS),windows)
	if exist bin rmdir /s /q bin
else
	rm -rf bin/
endif

init:
	@touch .env
	@if grep -q '^DOCUMCP_SECRET_KEY=' .env; then \
		echo "DOCUMCP_SECRET_KEY already in .env, skipping"; \
	else \
		echo "DOCUMCP_SECRET_KEY=$$(openssl rand -hex 32)" >> .env; \
		echo "Added DOCUMCP_SECRET_KEY to .env"; \
	fi
	@echo "config.yaml is optional. Copy config.example.yaml to config.yaml if you want declarative source seeding."

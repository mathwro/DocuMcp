# syntax=docker/dockerfile:1.7

# Stage 1: Build the Go binary on the native builder platform. Buildx provides
# TARGETOS/TARGETARCH for the requested output image platform.
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS builder

ARG TARGETOS
ARG TARGETARCH

RUN set -eux; \
    packages="gcc libsqlite3-dev"; \
    case "$TARGETARCH" in \
      amd64) ;; \
      arm64) packages="$packages gcc-aarch64-linux-gnu" ;; \
      *) echo "unsupported TARGETARCH: $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    apt-get update; \
    apt-get install -y $packages; \
    rm -rf /var/lib/apt/lists/*

WORKDIR /build
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    case "$TARGETARCH" in \
      amd64) export CC=gcc ;; \
      arm64) export CC=aarch64-linux-gnu-gcc ;; \
      *) echo "unsupported TARGETARCH: $TARGETARCH" >&2; exit 1 ;; \
    esac \
    && CGO_ENABLED=1 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags sqlite_fts5 -o /documcp ./cmd/documcp

# Stage 2: Download and export the ONNX model
FROM --platform=$BUILDPLATFORM python:3.12-slim AS model-downloader

# Pin versions to ensure reproducible model exports.
# Check https://github.com/huggingface/optimum for latest stable version.
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install "optimum[onnxruntime]"

RUN optimum-cli export onnx \
    --model sentence-transformers/all-MiniLM-L6-v2 \
    --task feature-extraction \
    /models/all-MiniLM-L6-v2

# Stage 3: Install CA certificates once on the native builder platform. The
# certificate bundle is architecture-independent and avoids emulated apt work.
FROM --platform=$BUILDPLATFORM debian:bookworm-slim AS certs

RUN apt-get update \
    && apt-get install -y ca-certificates \
    && mkdir -p /empty-data \
    && rm -rf /var/lib/apt/lists/*

# Stage 4: Minimal runtime image
FROM debian:bookworm-slim

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --chown=1001:1001 --from=builder /documcp /usr/local/bin/documcp
COPY --chown=1001:1001 --from=model-downloader /models /app/models
COPY --chown=1001:1001 --from=certs /empty-data /app/data

WORKDIR /app

USER 1001:1001

EXPOSE 8080

# The binary binds to 127.0.0.1 by default to avoid exposing a fresh install on
# a host's LAN. Inside the container we must bind all interfaces so port
# forwarding (-p 8080:8080) reaches the process. Override at `docker run` time
# with -e DOCUMCP_BIND_ADDR=... if a different address/port is needed.
ENV DOCUMCP_BIND_ADDR=0.0.0.0:8080

# /app/data  — SQLite database and encrypted token store
# Bind-mount /app/config.yaml from the host if you want declarative source
# seeding; otherwise the binary uses built-in defaults.
VOLUME ["/app/data"]

CMD ["documcp"]

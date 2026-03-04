# Stage 1: Build the Go binary
FROM golang:1.26-bookworm AS builder

RUN apt-get update && apt-get install -y gcc libsqlite3-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -tags sqlite_fts5 -o /documcp ./cmd/documcp

# Stage 2: Download and export the ONNX model
FROM python:3.12-slim AS model-downloader

# Pin versions to ensure reproducible model exports.
# Check https://github.com/huggingface/optimum for latest stable version.
RUN pip install --no-cache-dir "optimum[onnxruntime]"

RUN optimum-cli export onnx \
    --model sentence-transformers/all-MiniLM-L6-v2 \
    --task feature-extraction \
    /models/all-MiniLM-L6-v2

# Stage 3: Minimal runtime image
FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /documcp /usr/local/bin/documcp
COPY --from=model-downloader /models /app/models

RUN mkdir -p /app/data

RUN groupadd --gid 1001 documcp \
    && useradd --uid 1001 --gid 1001 --no-create-home documcp \
    && chown -R documcp:documcp /app /usr/local/bin/documcp

USER documcp

EXPOSE 8080

# /app/data  — SQLite database and encrypted token store
# /app/config.yaml — source-of-truth config (mount from host)
VOLUME ["/app/data", "/app/config.yaml"]

CMD ["documcp"]

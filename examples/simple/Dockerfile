# syntax=docker/dockerfile:1

# ------------------------------------------------------------------ builder ---
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /build

# Copy go.mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the example binary
WORKDIR /build/examples/simple
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/app .

# ------------------------------------------------------------------ runtime ---
FROM alpine:3.21

RUN apk add --no-cache ca-certificates

# Create non-root user
RUN addgroup -g 1000 app && \
    adduser -u 1000 -G app -s /bin/sh -D app

# Create data directory for .env file storage
RUN mkdir -p /data && chown app:app /data

COPY --from=builder /out/app /usr/local/bin/app

USER app
WORKDIR /data

EXPOSE 8080

ENV PORT=8080
ENV STORAGE_MODE=envfile
ENV STORAGE_DIR=/data/.env

CMD ["app"]

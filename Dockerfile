# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build static binary with cache mount for faster rebuilds
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -o pbr ./cmd/pbr/

# Runtime stage
FROM alpine:3.20.1

RUN apk add --no-cache fuse=2.9.9-r5

ARG USERNAME=pbruser
ARG USER_UID=1000
ARG USER_GID=$USER_UID

RUN addgroup -S -g $USER_GID $USERNAME \
  && adduser -S -u $USER_GID -G $USERNAME $USERNAME

COPY --from=builder /build/pbr /app/pbr

# Create data directory with proper permissions
RUN mkdir -p /data && chown -R $USERNAME:$USERNAME /data

WORKDIR /app

ENTRYPOINT ["/app/pbr"]

USER $USERNAME

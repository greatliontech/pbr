# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

PBR (Protobuf Registry) is a buf CLI-compatible protobuf registry server written in Go. It serves protobuf modules from Git repositories and supports code generation through containerized plugins.

## Build and Development Commands

```bash
# Build the binary
go build -o pbr ./cmd/pbr/

# Run tests
go test ./...

# Run a specific test
go test ./internal/config -run TestParseValidConfig

# Run with verbose output
go test -v ./...

# Tidy dependencies
go mod tidy
```

## Architecture

### Core Components

- **cmd/pbr/main.go**: Entry point. Loads config, sets up logging/telemetry, starts the HTTP service.

- **internal/service/**: Implements buf registry Connect RPC handlers (CommitService, GraphService, DownloadService, ModuleService, OwnerService, CodeGenerationService). The `Service` struct holds all state including modules, plugins, credentials, and user tokens.

- **internal/registry/**: Module management layer. `Registry` maps configured modules to `Module` instances. `Module` wraps a `Repository` and handles commit/file lookups, buf.lock parsing, and manifest digest computation using SHAKE256.

- **internal/repository/**: Git operations using go-git. Manages bare repositories with optional shallow cloning, fetches specific refs, caches commits, and returns files filtered by glob patterns.

- **internal/codegen/**: Protobuf plugin execution. `Plugin` mounts OCI images via OCIFS, runs plugins in Linux containers (user namespaces), and handles CodeGeneratorRequest/Response protobuf I/O. The `Runner` interface abstracts container execution.

- **internal/config/**: YAML configuration parsing with environment variable substitution (using `envsubst`).

### Key Patterns

- Modules are identified by `owner/name` and map to Git repositories with optional subpaths
- Commit IDs are truncated to 32 characters (first half of Git SHA)
- File digests use SHAKE256 for buf manifest compatibility
- Authentication supports tokens, SSH keys, basic auth, and GitHub App credentials
- Container registry credentials can be configured for pulling plugin images

### Configuration

The server expects a YAML config file (default `/config/config.yaml`) with:
- `host`: Registry hostname (used in buf.lock references)
- `address`: Listen address (default `:443`)
- `modules`: Map of `owner/module` to git remote/path/filters
- `plugins`: Map of plugin names to OCI images
- `credentials`: Git and container registry auth (supports `${ENV_VAR}` substitution)
- `users`: Map of usernames to tokens for authentication

## Deployment

- Docker image built with goreleaser, published to `ghcr.io/greatliontech/pbr`
- Helm chart in `chart/` directory
- Requires FUSE support for OCIFS image mounting

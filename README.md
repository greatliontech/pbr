# PBR - Protocol Buffer Registry

A self-hosted Buf Schema Registry (BSR) compatible server. PBR allows you to host your own protobuf module registry that works seamlessly with the `buf` CLI.

## Features

- Full compatibility with `buf push`, `buf dep update`, and `buf generate`
- Support for both buf.yaml v1 and v2 formats
- B4 and B5 digest support
- Multi-module workspace support (buf.yaml v2)
- Remote code generation with OCI-based plugins
- Git repository mirroring for protobuf modules
- CAS (Content Addressable Storage) with pluggable backends
- TLS support (native or via proxy)

## Quick Start

### Installation

```bash
go install github.com/greatliontech/pbr@latest
```

### Basic Configuration

Create a `config.yaml`:

```yaml
host: "pbr.example.com"
address: ":8080"

# Storage configuration
storage:
  blob_url: "file:///var/lib/pbr/blobs"
  docstore_url: "mem://"

# Optional: TLS configuration
tls:
  certfile: /path/to/server.crt
  keyfile: /path/to/server.key

# Optional: Authentication
users:
  myuser: "${USER_PASSWORD}"

admintoken: "${ADMIN_TOKEN}"
```

### Running

```bash
pbr serve --config config.yaml
```

### Using with buf CLI

Configure buf to use your registry:

```bash
# Login to your registry
buf registry login pbr.example.com

# Push a module
buf push --create

# Use as a dependency
# In buf.yaml:
# deps:
#   - pbr.example.com/myorg/mymodule
buf dep update
```

## Configuration Reference

### Server Settings

| Field | Description | Default |
|-------|-------------|---------|
| `host` | Public hostname of the registry | required |
| `address` | Listen address | `:8080` |
| `loglevel` | Log level (debug, info, warn, error) | `info` |
| `cachedir` | Directory for cache files | system temp |

### Storage

PBR uses [Go Cloud](https://gocloud.dev/) for storage backends:

```yaml
storage:
  # Blob storage for module content
  blob_url: "file:///var/lib/pbr/blobs"  # Local filesystem
  # blob_url: "s3://my-bucket?region=us-east-1"  # AWS S3
  # blob_url: "gs://my-bucket"  # Google Cloud Storage

  # Document store for metadata
  docstore_url: "mem://"  # In-memory (persisted to cachedir on shutdown)
  # docstore_url: "firestore://project/collection"  # Firestore
```

### TLS Configuration

#### File-based (recommended for Kubernetes)

```yaml
tls:
  certfile: /path/to/server.crt
  keyfile: /path/to/server.key
```

#### PEM strings with environment variable support

```yaml
tls:
  certpem: "${TLS_CERT}"
  keypem: "${TLS_KEY}"
```

### Authentication

```yaml
# Basic auth users
users:
  username1: "password1"
  username2: "${PASSWORD_FROM_ENV}"

# Admin token for management operations
admintoken: "${ADMIN_TOKEN}"

# Disable login requirement (not recommended for production)
nologin: false
```

### Git Mirroring

Mirror protobuf modules from Git repositories:

```yaml
modules:
  myorg/mymodule:
    remote: "https://github.com/myorg/myrepo.git"
    path: "proto"  # Subdirectory containing protos
    filters:
      - "api/**"   # Optional: only include matching paths
    shallow: true  # Optional: shallow clone

credentials:
  git:
    "github.com/*":
      token: "${GITHUB_TOKEN}"
    "gitlab.com/*":
      sshkey: "${SSH_PRIVATE_KEY}"
```

### Remote Code Generation

Configure OCI-based plugins for remote code generation:

```yaml
plugins:
  protocolbuffers/go:
    image: "ghcr.io/myorg/protoc-gen-go"
    default: "v1.35.2"
  grpc/go:
    image: "ghcr.io/myorg/protoc-gen-go-grpc"
    default: "v1.5.1"

credentials:
  containerregistry:
    "ghcr.io":
      username: "${GHCR_USER}"
      password: "${GHCR_TOKEN}"
```

## API Compatibility

PBR implements the following Buf Registry APIs:

### Module Services (v1 - for buf.yaml v2 / B5 digests)

| Service | Status |
|---------|--------|
| `ModuleService` | Implemented |
| `UploadService` | Implemented |
| `DownloadService` | Implemented |
| `GraphService` | Implemented |
| `CommitService` | Implemented |

### Module Services (v1beta1 - for buf.yaml v1 / B4 digests)

| Service | Status |
|---------|--------|
| `DownloadService` | Implemented |
| `GraphService` | Implemented |
| `CommitService` | Implemented |

### Other Services

| Service | Status |
|---------|--------|
| `OwnerService` (v1) | Implemented |
| `CodeGenerationService` (v1alpha1) | Implemented |

## buf.yaml v1 vs v2

PBR supports both configuration versions:

### v1 Format

```yaml
version: v1
name: pbr.example.com/myorg/mymodule
deps:
  - pbr.example.com/myorg/other
```

Uses B4 digests (`shake256:...`)

### v2 Format

```yaml
version: v2
modules:
  - path: .
    name: pbr.example.com/myorg/mymodule
deps:
  - pbr.example.com/myorg/other
```

Uses B5 digests (`b5:...`) which include dependency information.

### Multi-module Workspaces (v2 only)

```yaml
version: v2
modules:
  - path: proto/common
    name: pbr.example.com/myorg/common
  - path: proto/api
    name: pbr.example.com/myorg/api
deps:
  - buf.build/googleapis/googleapis
```

## Development

### Prerequisites

- Go 1.24+
- Docker (for e2e tests)

### Building

```bash
go build ./...
```

### Testing

```bash
# Unit tests
go test ./...

# E2E tests (requires Docker)
go test ./e2e/... -v
```

### Debug Mode

Enable HTTP request logging:

```bash
PBR_DEBUG_HTTP=1 pbr serve --config config.yaml
```

## License

Apache 2.0

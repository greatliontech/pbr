# PBR - Protocol Buffer Registry

A `buf` CLI compatible server.

## Features

- Full compatibility with `buf push`, `buf dep update`, `buf generate`, and `buf login`
- Support for both buf.yaml v1 and v2 formats
- Multi-module workspace support (buf.yaml v2)
- Remote code generation with OCI-based plugins
- Configurable storage backends via Go Cloud (local filesystem, S3, GCS, etc.)
- TLS support (native or via proxy)
- OAuth2 device flow for `buf login` (interactive browser-based login)
- OIDC integration (Keycloak, Auth0, Okta, etc.)

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

### Authentication

PBR supports multiple authentication methods:

#### Simple Username/Password

```yaml
# Basic auth users (password is also the token)
users:
  username1: "password1"
  username2: "${PASSWORD_FROM_ENV}"

# Admin token for management operations
admintoken: "${ADMIN_TOKEN}"

# Disable login requirement (not recommended for production)
nologin: false
```

When using simple authentication, the password is used as the API token. Login with:

```bash
# Interactive login (opens browser)
buf registry login pbr.example.com

# Or with token directly
buf registry login pbr.example.com --token-stdin <<< "password1"
```

#### OIDC Integration

PBR can integrate with external identity providers via OpenID Connect:

```yaml
oidc:
  issuer: "https://keycloak.example.com/realms/myrealm"
  client_id: "pbr-registry"
  client_secret: "${OIDC_CLIENT_SECRET}"
  # Optional: custom scopes (default: openid, email, profile)
  scopes:
    - openid
    - email
    - profile
  # Optional: claim to use as username (default: preferred_username)
  username_claim: preferred_username
```

With OIDC configured, `buf login` will redirect users to your identity provider for authentication.

#### OAuth2 Device Flow

PBR implements the OAuth2 Device Authorization Grant (RFC 8628) for `buf login`:

1. User runs `buf registry login pbr.example.com`
2. buf CLI requests a device code from PBR
3. User opens the verification URL in a browser
4. User authenticates (via OIDC or username/password form)
5. buf CLI receives the access token

This flow works well for CLI tools and headless environments.

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
| `AuthnService` (v1alpha1) | Implemented |
| `CodeGenerationService` (v1alpha1) | Implemented |

### OAuth2 Endpoints

| Endpoint | Description |
|----------|-------------|
| `POST /oauth2/device/registration` | Register a new device client |
| `POST /oauth2/device/authorization` | Request device authorization |
| `POST /oauth2/device/token` | Poll for access token |
| `GET /oauth2/device/approve` | User approval page |
| `GET /oauth2/oidc/callback` | OIDC callback handler |

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

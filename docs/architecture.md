# PBR Architecture

This document describes the internal architecture of PBR.

## Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         buf CLI                                  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼ Connect RPC (HTTP/2)
┌─────────────────────────────────────────────────────────────────┐
│                      PBR Server                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │                    Service Layer                          │   │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐    │   │
│  │  │  Module  │ │  Upload  │ │ Download │ │  Graph   │    │   │
│  │  │ Service  │ │ Service  │ │ Service  │ │ Service  │    │   │
│  │  └──────────┘ └──────────┘ └──────────┘ └──────────┘    │   │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐                 │   │
│  │  │  Commit  │ │  Owner   │ │ CodeGen  │                 │   │
│  │  │ Service  │ │ Service  │ │ Service  │                 │   │
│  │  └──────────┘ └──────────┘ └──────────┘                 │   │
│  └─────────────────────────────────────────────────────────┘   │
│                              │                                   │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │                    Registry Layer                         │   │
│  │  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐     │   │
│  │  │    Module    │ │    Commit    │ │    Owner     │     │   │
│  │  └──────────────┘ └──────────────┘ └──────────────┘     │   │
│  └─────────────────────────────────────────────────────────┘   │
│                              │                                   │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │                    Storage Layer                          │   │
│  │  ┌──────────────┐ ┌──────────────┐                       │   │
│  │  │  Blob Store  │ │  Doc Store   │                       │   │
│  │  │   (files)    │ │  (metadata)  │                       │   │
│  │  └──────────────┘ └──────────────┘                       │   │
│  └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┴───────────────┐
              ▼                               ▼
     ┌─────────────┐                 ┌─────────────┐
     │ Filesystem  │                 │   S3/GCS    │
     │    Blobs    │                 │    Blobs    │
     └─────────────┘                 └─────────────┘
```

## Layers

### Service Layer

Implements the Buf Registry API using Connect RPC:

| Service | Version | Description |
|---------|---------|-------------|
| `ModuleService` | v1 | Module CRUD operations |
| `UploadService` | v1 | Push modules to registry |
| `DownloadService` | v1, v1beta1 | Download module content |
| `GraphService` | v1, v1beta1 | Dependency graph resolution |
| `CommitService` | v1, v1beta1 | Commit operations |
| `OwnerService` | v1 | Owner (organization) operations |
| `AuthnService` | v1alpha1 | Authentication (user info) |
| `CodeGenerationService` | v1alpha1 | Remote code generation |

### Registry Layer

Business logic for managing modules and commits:

- **Module**: Represents a protobuf module with its metadata
- **Commit**: A specific version of a module with files and dependencies
- **Owner**: Organization or user that owns modules

### Storage Layer

Pluggable storage backends using Go Cloud:

- **Blob Store**: Stores module file content (proto files, manifests)
- **Doc Store**: Stores metadata (modules, commits, owners, labels)

## Key Concepts

### Content Addressable Storage (CAS)

All module content is stored by digest:

1. Files are hashed with SHAKE256
2. A manifest lists all files with their digests
3. The manifest is hashed to create the module digest
4. Content is stored by digest, enabling deduplication

### Digest Types

| Type | Format | Description |
|------|--------|-------------|
| B4 | `shake256:<hex>` | Legacy, files only |
| B5 | `b5:<hex>` | Current, files + dependencies |

See [hashing.md](hashing.md) for detailed digest calculation.

### Module Resolution

When resolving a module reference:

1. Parse reference (owner/module:label or owner/module:commit)
2. Look up module in registry
3. Resolve label to commit ID (if label provided)
4. Fetch commit metadata and files

### Dependency Resolution

The GraphService builds a complete dependency graph:

1. Start with requested module(s)
2. For each module, read its buf.lock for dependencies
3. Recursively resolve all dependencies
4. Handle version conflicts (newer wins)
5. Return graph with all commits and edges

## Authentication

PBR supports multiple authentication methods for `buf login`:

### OAuth2 Device Flow (OIDC Proxy Mode)

When OIDC is configured, PBR proxies the OAuth2 device flow to the external identity provider:

```
buf login pbr.example.com
        │
        ▼
POST /oauth2/device/registration
        │ (returns configured OIDC client_id)
        ▼
POST /oauth2/device/authorization ──► Proxied to OIDC provider
        │ (get device_code, user_code, verification_uri)
        │
        ├─ User opens verification_uri (at OIDC provider)
        │   └─ User authenticates with IdP
        │
        └─ buf CLI polls POST /oauth2/device/token
            │
            ▼
        PBR proxies to OIDC token endpoint
            │
            ▼
        On success: PBR calls userinfo, generates PBR token
            │
            ▼
        Return PBR access_token (not OIDC token)
```

### Token Validation

All API requests (except OAuth2 endpoints) require authentication:

1. Client sends `Authorization: Bearer <token>` header
2. Auth interceptor validates token against stored tokens
3. Checks token expiration (OIDC tokens expire, static tokens don't)
4. Slides expiration on valid requests (extends token lifetime)
5. On success, user context is set for the request

### Token Expiration

- **Static tokens** (from `users:` config): Never expire
- **OIDC tokens**: Expire after configured TTL (default: 7 days)
- **Sliding expiration**: Each API call extends the token's lifetime
- **Re-login**: Replaces the old token with a new one

## Code Generation

Remote code generation flow:

```
buf generate (remote plugin)
        │
        ▼
CodeGenerationService.GenerateCode
        │
        ├─ Parse FileDescriptorSet from request
        │
        ├─ Look up plugin from config
        │
        ├─ Pull plugin OCI image
        │
        ├─ Run plugin in container
        │   ├─ Mount image filesystem
        │   ├─ Send CodeGeneratorRequest to stdin
        │   └─ Read CodeGeneratorResponse from stdout
        │
        └─ Return generated files
```

## Directory Structure

```
internal/
├── codegen/          # Code generation with OCI plugins
│   ├── plugin.go     # Plugin execution
│   └── runner.go     # Container runtime
├── config/           # Configuration parsing
│   └── config.go     # Config struct and parsing
├── registry/         # Module/commit/owner logic
│   └── module.go     # Module operations
├── service/          # Connect RPC handlers
│   ├── service.go    # Service setup and auth interceptor
│   ├── authn.go      # AuthnService (user info)
│   ├── oauth2.go     # OAuth2 device flow endpoints
│   ├── oidc.go       # OIDC provider integration
│   ├── upload.go     # UploadService
│   ├── download.go   # DownloadService (v1beta1)
│   ├── download_v1.go # DownloadService (v1)
│   ├── graph.go      # GraphService (v1beta1)
│   ├── graph_v1.go   # GraphService (v1)
│   ├── commit.go     # CommitService (v1beta1)
│   ├── commit_v1.go  # CommitService (v1)
│   ├── module.go     # ModuleService
│   └── code-generation.go # CodeGenerationService
├── storage/          # Storage abstraction
│   ├── storage.go    # Interfaces
│   ├── docstore.go   # Docstore implementation
│   └── manifest.go   # Manifest/digest handling
└── util/             # Utility functions
```

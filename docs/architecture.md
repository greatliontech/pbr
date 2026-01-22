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
├── registry/         # Module/commit/owner logic
│   └── module.go     # Module operations
├── service/          # Connect RPC handlers
│   ├── service.go    # Service setup
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

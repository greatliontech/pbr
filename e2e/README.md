# E2E Testing Guide

End-to-end tests for PBR using the buf CLI and testcontainers.

## Overview

The e2e tests spin up PBR in Docker containers and run buf CLI commands against it.
All tests run in isolation with their own Docker networks and certificates - no host
system configuration required.

## Test Modes

Two TLS modes are tested:

1. **Envoy TLS** (`TestE2EEnvoyTLS`) - Envoy terminates TLS, proxies HTTP/2 to PBR
2. **Native TLS** (`TestE2ENativeTLS`) - PBR handles TLS directly

## Running Tests

```bash
# Run all e2e tests
go test ./e2e/... -v

# Run only Envoy TLS mode
go test ./e2e/... -v -run TestE2EEnvoyTLS

# Run only Native TLS mode
go test ./e2e/... -v -run TestE2ENativeTLS

# Run only v2 tests
go test ./e2e/... -v -run "V2"

# Skip e2e tests (short mode)
go test ./e2e/... -v -short
```

## Prerequisites

- Docker (for testcontainers)
- Go 1.24+

## Directory Structure

```
e2e/
├── e2e_test.go       # Main test file with testcontainers setup
├── README.md         # This file
└── testdata/         # Test proto modules
    ├── basic/        # Simple module (v1 format)
    ├── deps/         # Module with dependencies (v1 format)
    ├── labels/       # Version labels test
    ├── nested/       # Nested dependency chain
    │   ├── base/
    │   ├── mid-a/
    │   ├── mid-b/
    │   └── top/
    ├── pinned/       # Pinned dependency versions
    │   ├── base/
    │   └── consumer/
    ├── v2basic/      # Simple module (v2 format, B5 digests)
    ├── v2deps/       # Module with dependencies (v2 format)
    └── v2multi/      # Multi-module workspace (v2 format)
        ├── common/
        └── service/
```

## Test Coverage

### buf.yaml v1 Tests

| Test | Description |
|------|-------------|
| `BasicModule` | Push and retrieve a simple module |
| `ModuleWithDependencies` | Module depending on another module |
| `Labels` | Push with version labels (main, v1.0.0) |
| `NestedDependencies` | Multi-level dependency chain |
| `PinnedDependencies` | Pin to specific commit versions |

### buf.yaml v2 Tests

| Test | Description |
|------|-------------|
| `V2BasicModule` | Single module with v2 format |
| `V2Dependencies` | V2 module with dependencies (B5 digests) |
| `V2MultiModule` | Multi-module workspace |

## Architecture

### Envoy TLS Mode

```
buf CLI (HTTPS :443)
        │
        ▼
Envoy Proxy (TLS termination + HTTP/2)
        │
        ▼ HTTP/2 :8080
PBR Server (Connect RPC)
        │
        ▼
CAS Storage
```

### Native TLS Mode

```
buf CLI (HTTPS :443)
        │
        ▼
PBR Server (TLS + Connect RPC)
        │
        ▼
CAS Storage
```

## TLS Configuration

PBR supports TLS through two methods:

### File-based (recommended for Kubernetes)

```yaml
tls:
  certfile: /path/to/server.crt
  keyfile: /path/to/server.key
```

### PEM strings (env var support)

```yaml
tls:
  certpem: ${TLS_CERT}
  keypem: ${TLS_KEY}
```

This is useful when mounting Kubernetes secrets as environment variables.

## Debugging Failed Tests

Enable PBR debug logging by setting `PBR_DEBUG_HTTP=1` in the test environment.
The test will dump PBR container logs on failure.

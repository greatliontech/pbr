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
    ├── basic/        # Simple module
    ├── deps/         # Module with dependencies
    └── labels/       # Version labels test
```

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

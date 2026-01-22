# Module Digest Hashing in PBR

This document explains how module digests are calculated in PBR, which is compatible with the Buf CLI's digest system. PBR supports two digest types: **b4** (legacy) and **b5** (current).

## Overview

Both digest types use **SHAKE256** as the underlying hash function, producing a 64-byte (512-bit) digest. The difference lies in *what* is hashed:

| Digest Type | String Prefix | Includes Dependencies | Status |
|-------------|---------------|----------------------|--------|
| b4 | `shake256:` | No | Legacy |
| b5 | `b5:` | Yes | Current |

## Hash Function: SHAKE256

SHAKE256 is an extendable-output function (XOF) from the SHA-3 family. PBR uses it to produce 64-byte digests:

```go
import "golang.org/x/crypto/sha3"

func shake256Hash(content []byte) [64]byte {
    h := sha3.NewShake256()
    h.Write(content)
    var hashBytes [64]byte
    h.Read(hashBytes[:])
    return hashBytes
}
```

## File Digests

Individual files are hashed by reading their content through SHAKE256:

```
file content → SHAKE256 → 64-byte digest → "shake256:<hex>"
```

Example:
```
shake256:7c88a20cf931702d042a4ddee3fde5de84814544411f1c62dbf435b1b81a12a8866a070baabcf8b5a0d31675af361ccb2d93ddada4cdcc11bab7ea3d8d7c4667
```

## Manifest Format

A manifest is a sorted list of file entries, where each entry contains:
- The file's SHAKE256 digest
- Two spaces as separator
- The file's path (normalized, relative to module root)

Format per line:
```
<digest>[SP][SP]<path>\n
```

Example manifest:
```
shake256:cd22db48cf7c274bbffcb5494a854000cd21b074df7c6edabbd0102c4be8d7623e3931560fcda7acfab286ae1d4f506911daa31f223ee159f59ffce0c7acbbaa  buf.lock
shake256:3b353aa5aacd11015e8577f16e2c4e7a242ce773d8e3a16806795bb94f76e601b0db9bf42d5e1907fda63303e1fa1c65f1c175ecc025a3ef29c3456ad237ad84  buf.md
shake256:9db25155eafd19b36882cff129daac575baa67ee44d1cb1fd3894342b28c72b83eb21aa595b806e9cb5344759bc8308200c5af98e4329aa83014dde99afa903a  pet/v1/pet.proto
```

**Important**: Entries MUST be sorted alphabetically by path to ensure deterministic output.

---

## B4 Digest (Legacy)

The b4 digest hashes only the module's files plus optional configuration files (`buf.yaml`, `buf.lock`).

### Calculation Steps

1. **Collect all module files** (`.proto` files matching module configuration)
2. **For each file**, compute its SHAKE256 digest
3. **Create FileNodes**: pairs of `(path, digest)`
4. **Build a Manifest**: sorted list of FileNodes
5. **Optionally add** `buf.yaml` and `buf.lock` if present
6. **Serialize the manifest** to its canonical string form
7. **Hash the manifest string** with SHAKE256
8. **Prefix with `shake256:`**

### Pseudocode

```
function computeB4Digest(files, bufYAML, bufLock):
    fileNodes = []

    // Hash all module files
    for file in files:
        digest = shake256(file.content)
        fileNodes.append(FileNode{path: file.path, digest: digest})

    // Add config files if present
    if bufYAML != nil:
        digest = shake256(bufYAML.content)
        fileNodes.append(FileNode{path: "buf.yaml", digest: digest})

    if bufLock != nil:
        digest = shake256(bufLock.content)
        fileNodes.append(FileNode{path: "buf.lock", digest: digest})

    // Create and serialize manifest
    manifest = Manifest{entries: fileNodes}
    manifestString = serializeManifest(manifest)  // sorted by path

    // Final digest
    return "shake256:" + hex(shake256(manifestString))
```

### Visual Example

```
Module files:
  foo.proto  →  shake256:aaa...
  bar.proto  →  shake256:bbb...
  buf.yaml   →  shake256:ccc...

Manifest (sorted by path):
  shake256:bbb...  bar.proto
  shake256:ccc...  buf.yaml
  shake256:aaa...  foo.proto

Manifest string:
  "shake256:bbb...  bar.proto\nshake256:ccc...  buf.yaml\nshake256:aaa...  foo.proto\n"

B4 Digest = shake256(manifest_string)
         → shake256:xyz123...
```

---

## B5 Digest (Current)

The b5 digest is a **composite digest** that includes both the module's files AND the digests of all its dependencies. This makes it a true content-addressable identifier for the entire dependency tree.

### Key Difference from B4

- **B4**: Only hashes file contents (no dependency information)
- **B5**: Hashes file contents + all dependency digests

This means two modules with identical source files but different dependencies will have different b5 digests.

### Calculation Steps

1. **Compute the files digest** (similar to b4, but without config files):
   - Hash all module `.proto` files
   - Create a manifest (sorted by path)
   - Hash the manifest → `filesDigest`

2. **Collect dependency digests**:
   - For each dependency, get its b5 digest
   - Convert each to string format: `b5:<hex>`
   - Sort the strings alphabetically

3. **Combine and hash**:
   - Create a newline-separated string:
     - First line: `shake256:<filesDigestHex>`
     - Remaining lines: sorted dependency digest strings
   - Hash this combined string with SHAKE256
   - Prefix with `b5:`

### Pseudocode

```
function computeB5Digest(files, dependencies):
    // Step 1: Compute files digest
    fileNodes = []
    for file in files:
        digest = shake256(file.content)
        fileNodes.append(FileNode{path: file.path, digest: digest})

    manifest = Manifest{entries: fileNodes}
    manifestString = serializeManifest(manifest)  // sorted by path
    filesDigest = "shake256:" + hex(shake256(manifestString))

    // Step 2: Collect and sort dependency digests
    depDigestStrings = []
    for dep in dependencies:
        depDigest = dep.getB5Digest()  // recursively computed
        depDigestStrings.append(depDigest.String())  // "b5:<hex>"

    sort(depDigestStrings)  // alphabetical sort

    // Step 3: Combine and hash
    allDigests = [filesDigest] + depDigestStrings
    combined = join(allDigests, "\n")

    finalDigest = shake256(combined)
    return "b5:" + hex(finalDigest)
```

### Visual Example

```
Module files:
  foo.proto  →  shake256:aaa...
  bar.proto  →  shake256:bbb...

Manifest (sorted by path):
  shake256:bbb...  bar.proto
  shake256:aaa...  foo.proto

Files digest = shake256(manifest_string)
            → shake256:fff...

Dependencies (each with their own b5 digest):
  dep1  →  b5:111...
  dep2  →  b5:222...

Sorted dependency digests:
  b5:111...
  b5:222...

Combined string to hash:
  "shake256:fff...\nb5:111...\nb5:222..."

B5 Digest = shake256(combined)
         → b5:xyz789...
```

### Recursive Nature

Since b5 digests include dependency digests, and those dependencies also have b5 digests (which include *their* dependencies), the b5 digest effectively captures the entire transitive dependency tree.

```
Module A (b5:aaa)
├── depends on B (b5:bbb)
│   └── depends on D (b5:ddd)
└── depends on C (b5:ccc)
    └── depends on D (b5:ddd)  // same as above

A's b5 digest = f(A's files, B's b5, C's b5)
              = f(A's files, f(B's files, D's b5), f(C's files, D's b5))
```

---

## Implementation Notes

### Module Files vs Config Files

| Digest Type | `.proto` files | `buf.yaml` | `buf.lock` |
|-------------|----------------|------------|------------|
| b4 | ✓ | ✓ (if present) | ✓ (if present) |
| b5 | ✓ | ✗ | ✗ |

B5 doesn't include config files because:
1. `buf.yaml` configuration is module-specific metadata
2. `buf.lock` contents are captured via dependency digests

### Path Normalization

All paths must be:
- Relative to module root
- Forward-slash separated (even on Windows)
- Normalized (no `.` or `..` components)
- Non-empty

### Determinism Requirements

For reproducible digests:
1. File contents must be byte-identical
2. Manifest entries must be sorted alphabetically by path
3. Dependency digests must be sorted alphabetically
4. Line endings must be `\n` (LF), not `\r\n` (CRLF)

---

## Digest String Formats

### B4 Digest String
```
shake256:<128-character-hex-value>
```

Example:
```
shake256:7c88a20cf931702d042a4ddee3fde5de84814544411f1c62dbf435b1b81a12a8866a070baabcf8b5a0d31675af361ccb2d93ddada4cdcc11bab7ea3d8d7c4667
```

### B5 Digest String
```
b5:<128-character-hex-value>
```

Example:
```
b5:9db25155eafd19b36882cff129daac575baa67ee44d1cb1fd3894342b28c72b83eb21aa595b806e9cb5344759bc8308200c5af98e4329aa83014dde99afa903a
```

---

## Comparison Summary

| Aspect | B4 | B5 |
|--------|----|----|
| Hash algorithm | SHAKE256 (64 bytes) | SHAKE256 (64 bytes) |
| String prefix | `shake256:` | `b5:` |
| Includes proto files | Yes | Yes |
| Includes buf.yaml | Yes (if present) | No |
| Includes buf.lock | Yes (if present) | No |
| Includes dependency digests | No | Yes |
| Captures dep tree changes | No | Yes |
| Recommended for new code | No | Yes |

---

## References

- [SHAKE256 (NIST FIPS 202)](https://nvlpubs.nist.gov/nistpubs/FIPS/NIST.FIPS.202.pdf)
- [Go crypto/sha3 package](https://pkg.go.dev/golang.org/x/crypto/sha3)
- [Buf Module Digest Documentation](https://buf.build/docs/reference/digest)

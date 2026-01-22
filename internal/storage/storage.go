package storage

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
)

// DigestType represents the type of module digest.
type DigestType int

const (
	// DigestTypeB4 represents the legacy b4 digest type (files + config only).
	// String representation uses "shake256:" prefix for backwards compatibility.
	DigestTypeB4 DigestType = iota + 1
	// DigestTypeB5 represents the current b5 digest type (files + dependencies).
	// String representation uses "b5:" prefix.
	DigestTypeB5
)

// String returns the string representation of the digest type.
func (dt DigestType) String() string {
	switch dt {
	case DigestTypeB4:
		return "shake256"
	case DigestTypeB5:
		return "b5"
	default:
		return fmt.Sprintf("unknown(%d)", dt)
	}
}

// ParseDigestType parses a digest type from its string representation.
func ParseDigestType(s string) (DigestType, error) {
	switch s {
	case "shake256":
		return DigestTypeB4, nil
	case "b5":
		return DigestTypeB5, nil
	default:
		return 0, fmt.Errorf("unknown digest type: %q", s)
	}
}

// ModuleDigest represents a module digest with its type.
// Unlike Digest which is for content-addressable storage,
// ModuleDigest represents a composite digest of module files and dependencies.
type ModuleDigest struct {
	Type  DigestType
	Value []byte // 64 bytes for SHAKE256
}

// String returns the digest in the format "type:hex".
func (md ModuleDigest) String() string {
	return fmt.Sprintf("%s:%s", md.Type.String(), hex.EncodeToString(md.Value))
}

// Hex returns just the hex-encoded hash value.
func (md ModuleDigest) Hex() string {
	return hex.EncodeToString(md.Value)
}

// ParseModuleDigest parses a module digest string in the format "type:hex".
func ParseModuleDigest(s string) (ModuleDigest, error) {
	for i, c := range s {
		if c == ':' {
			typeStr := s[:i]
			hexStr := s[i+1:]
			dt, err := ParseDigestType(typeStr)
			if err != nil {
				return ModuleDigest{}, err
			}
			value, err := hex.DecodeString(hexStr)
			if err != nil {
				return ModuleDigest{}, fmt.Errorf("invalid module digest hex: %w", err)
			}
			if len(value) != 64 {
				return ModuleDigest{}, fmt.Errorf("invalid module digest length: expected 64 bytes, got %d", len(value))
			}
			return ModuleDigest{Type: dt, Value: value}, nil
		}
	}
	return ModuleDigest{}, fmt.Errorf("invalid module digest format: missing type prefix")
}

// Digest represents a content-addressable hash.
type Digest struct {
	Algorithm string // e.g., "shake256"
	Value     []byte // raw hash bytes (64 bytes for SHAKE256)
}

// String returns the digest in the format "algorithm:hex".
func (d Digest) String() string {
	return fmt.Sprintf("%s:%s", d.Algorithm, hex.EncodeToString(d.Value))
}

// Hex returns just the hex-encoded hash value.
func (d Digest) Hex() string {
	return hex.EncodeToString(d.Value)
}

// ShortHex returns the first n characters of the hex-encoded hash.
func (d Digest) ShortHex(n int) string {
	h := d.Hex()
	if len(h) < n {
		return h
	}
	return h[:n]
}

// ParseDigest parses a digest string in the format "algorithm:hex".
func ParseDigest(s string) (Digest, error) {
	for i, c := range s {
		if c == ':' {
			algorithm := s[:i]
			hexStr := s[i+1:]
			value, err := hex.DecodeString(hexStr)
			if err != nil {
				return Digest{}, fmt.Errorf("invalid digest hex: %w", err)
			}
			return Digest{Algorithm: algorithm, Value: value}, nil
		}
	}
	return Digest{}, fmt.Errorf("invalid digest format: missing algorithm prefix")
}

// ManifestEntry represents a single file in a manifest.
type ManifestEntry struct {
	Digest Digest
	Path   string
}

// Manifest represents a collection of files with their content digests.
type Manifest struct {
	Entries []ManifestEntry
}

// BlobStore is the interface for content-addressable blob storage.
type BlobStore interface {
	// Get retrieves a blob by its digest.
	// Returns ErrNotFound if the blob does not exist.
	Get(ctx context.Context, digest Digest) (io.ReadCloser, error)

	// Put stores a blob and returns its computed SHAKE256 digest.
	Put(ctx context.Context, r io.Reader) (Digest, error)

	// Exists checks if a blob with the given digest exists.
	Exists(ctx context.Context, digest Digest) (bool, error)

	// Delete removes a blob by its digest.
	// Returns nil if the blob does not exist.
	Delete(ctx context.Context, digest Digest) error
}

// ManifestStore manages manifests (collections of file blobs).
type ManifestStore interface {
	// GetManifest retrieves a manifest by its digest.
	// Returns ErrNotFound if the manifest does not exist.
	GetManifest(ctx context.Context, digest Digest) (*Manifest, error)

	// PutManifest stores a manifest and returns its computed digest.
	// The manifest digest is the SHAKE256 hash of the serialized manifest content.
	PutManifest(ctx context.Context, manifest *Manifest) (Digest, error)

	// Exists checks if a manifest with the given digest exists.
	Exists(ctx context.Context, digest Digest) (bool, error)
}

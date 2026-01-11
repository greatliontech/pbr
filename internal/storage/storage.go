package storage

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
)

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

package storage

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"
	"golang.org/x/crypto/sha3"
)

// ManifestStoreImpl implements ManifestStore using gocloud.dev/blob.
// Manifests are stored at: manifests/<algorithm>/<first-2-hex>/<full-hex-digest>
type ManifestStoreImpl struct {
	bucket *blob.Bucket
}

// NewManifestStore creates a new gocloud.dev/blob-backed manifest store.
func NewManifestStore(bucket *blob.Bucket) *ManifestStoreImpl {
	return &ManifestStoreImpl{bucket: bucket}
}

// manifestKey returns the key for a given digest.
func manifestKey(digest Digest) string {
	hex := digest.Hex()
	if len(hex) < 2 {
		return "manifests/" + digest.Algorithm + "/" + hex
	}
	return "manifests/" + digest.Algorithm + "/" + hex[:2] + "/" + hex
}

// SerializeManifest converts a manifest to the buf-compatible format.
// Format: "shake256:<hex-digest>  <path>\n" for each entry, sorted by path.
func SerializeManifest(m *Manifest) string {
	// Sort entries by path for deterministic output
	entries := make([]ManifestEntry, len(m.Entries))
	copy(entries, m.Entries)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})

	var sb strings.Builder
	for _, entry := range entries {
		sb.WriteString(fmt.Sprintf("%s  %s\n", entry.Digest.String(), entry.Path))
	}
	return sb.String()
}

// ParseManifest parses the buf-compatible manifest format.
func ParseManifest(content string) (*Manifest, error) {
	m := &Manifest{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// Format: "algorithm:hex  path"
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid manifest line: %q", line)
		}
		digest, err := ParseDigest(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid digest in manifest: %w", err)
		}
		m.Entries = append(m.Entries, ManifestEntry{
			Digest: digest,
			Path:   parts[1],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

// computeManifestDigest computes the SHAKE256 digest of the given content.
func computeManifestDigest(content string) Digest {
	h := sha3.NewShake256()
	h.Write([]byte(content))
	var hashBytes [64]byte
	h.Read(hashBytes[:])
	return Digest{
		Algorithm: "shake256",
		Value:     hashBytes[:],
	}
}

// GetManifest retrieves a manifest by its digest.
func (s *ManifestStoreImpl) GetManifest(ctx context.Context, digest Digest) (*Manifest, error) {
	key := manifestKey(digest)
	r, err := s.bucket.NewReader(ctx, key, nil)
	if err != nil {
		if gcerrors.Code(err) == gcerrors.NotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer r.Close()

	content, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return ParseManifest(string(content))
}

// PutManifest stores a manifest and returns its computed digest.
func (s *ManifestStoreImpl) PutManifest(ctx context.Context, manifest *Manifest) (Digest, error) {
	content := SerializeManifest(manifest)
	digest := computeManifestDigest(content)

	key := manifestKey(digest)

	// Check if already exists (deduplication)
	exists, err := s.bucket.Exists(ctx, key)
	if err != nil {
		return Digest{}, err
	}
	if exists {
		return digest, nil
	}

	// Write the manifest
	w, err := s.bucket.NewWriter(ctx, key, nil)
	if err != nil {
		return Digest{}, err
	}

	if _, err := io.Copy(w, bytes.NewReader([]byte(content))); err != nil {
		w.Close()
		return Digest{}, err
	}

	if err := w.Close(); err != nil {
		return Digest{}, err
	}

	return digest, nil
}

// Exists checks if a manifest with the given digest exists.
func (s *ManifestStoreImpl) Exists(ctx context.Context, digest Digest) (bool, error) {
	key := manifestKey(digest)
	return s.bucket.Exists(ctx, key)
}

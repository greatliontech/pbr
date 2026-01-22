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
type ManifestStoreImpl struct {
	bucket *blob.Bucket
}

// NewManifestStore creates a new gocloud.dev/blob-backed manifest store.
func NewManifestStore(bucket *blob.Bucket) *ManifestStoreImpl {
	return &ManifestStoreImpl{bucket: bucket}
}

// manifestPath returns the storage path for a given digest.
func manifestPath(digest Digest) string {
	return "manifests/" + digest.Algorithm + "/" + digest.Hex()
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

// computeFilesDigest computes the SHAKE256 digest of the serialized manifest content.
func computeFilesDigest(content string) Digest {
	h := sha3.NewShake256()
	h.Write([]byte(content))
	var hashBytes [64]byte
	h.Read(hashBytes[:])
	return Digest{
		Algorithm: DigestAlgorithmShake256,
		Value:     hashBytes[:],
	}
}

// GetManifest retrieves a manifest by its digest.
func (s *ManifestStoreImpl) GetManifest(ctx context.Context, digest Digest) (*Manifest, error) {
	key := manifestPath(digest)
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
	digest := computeFilesDigest(content)

	key := manifestPath(digest)

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
	key := manifestPath(digest)
	return s.bucket.Exists(ctx, key)
}

// ComputeB5Digest computes a B5 module digest from a manifest and dependency digests.
//
// The B5 digest is computed as follows:
// 1. Compute the SHAKE256 digest of the serialized manifest (files digest)
// 2. Collect all dependency B5 digests and sort them alphabetically
// 3. Combine: filesDigest + "\n" + sorted dependency digests (joined by "\n")
// 4. Compute SHAKE256 of the combined string
//
// The result captures both the module's files and its entire dependency tree.
func ComputeB5Digest(manifest *Manifest, depDigests []ModuleDigest) (ModuleDigest, error) {
	// Step 1: Compute the files digest (SHAKE256 of serialized manifest)
	manifestContent := SerializeManifest(manifest)
	filesDigest := computeFilesDigest(manifestContent)

	// Step 2: Collect and sort dependency digest strings
	depDigestStrings := make([]string, 0, len(depDigests))
	for _, d := range depDigests {
		if d.Type != DigestTypeB5 {
			return ModuleDigest{}, fmt.Errorf("dependency digest must be B5, got %s", d.Type)
		}
		depDigestStrings = append(depDigestStrings, d.String())
	}
	sort.Strings(depDigestStrings)

	// Step 3: Combine files digest with sorted dependency digests
	// Format: "shake256:<files_hex>\nb5:<dep1_hex>\nb5:<dep2_hex>..."
	allDigests := make([]string, 0, 1+len(depDigestStrings))
	allDigests = append(allDigests, filesDigest.String())
	allDigests = append(allDigests, depDigestStrings...)
	combined := strings.Join(allDigests, "\n")

	// Step 4: Compute SHAKE256 of the combined string
	h := sha3.NewShake256()
	h.Write([]byte(combined))
	var hashBytes [64]byte
	h.Read(hashBytes[:])

	return ModuleDigest{
		Type:  DigestTypeB5,
		Value: hashBytes[:],
	}, nil
}

// ComputeB4Digest computes a B4 (legacy) module digest from a manifest.
//
// The B4 digest is simply the SHAKE256 hash of the serialized manifest.
// It does not include dependency information.
func ComputeB4Digest(manifest *Manifest) ModuleDigest {
	manifestContent := SerializeManifest(manifest)
	digest := computeFilesDigest(manifestContent)
	return ModuleDigest{
		Type:  DigestTypeB4,
		Value: digest.Value,
	}
}

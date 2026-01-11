package filesystem

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/greatliontech/pbr/internal/storage"
	"golang.org/x/crypto/sha3"
)

// ManifestStore implements storage.ManifestStore using the filesystem.
// Manifests are stored at: <basePath>/<algorithm>/<first-2-hex>/<full-hex-digest>
type ManifestStore struct {
	basePath string
}

// NewManifestStore creates a new filesystem-backed manifest store.
func NewManifestStore(basePath string) *ManifestStore {
	return &ManifestStore{basePath: basePath}
}

// manifestPath returns the filesystem path for a given digest.
func (s *ManifestStore) manifestPath(digest storage.Digest) string {
	hex := digest.Hex()
	if len(hex) < 2 {
		return filepath.Join(s.basePath, digest.Algorithm, hex)
	}
	return filepath.Join(s.basePath, digest.Algorithm, hex[:2], hex)
}

// SerializeManifest converts a manifest to the buf-compatible format.
// Format: "shake256:<hex-digest>  <path>\n" for each entry, sorted by path.
func SerializeManifest(m *storage.Manifest) string {
	// Sort entries by path for deterministic output
	entries := make([]storage.ManifestEntry, len(m.Entries))
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
func ParseManifest(content string) (*storage.Manifest, error) {
	m := &storage.Manifest{}
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
		digest, err := storage.ParseDigest(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid digest in manifest: %w", err)
		}
		m.Entries = append(m.Entries, storage.ManifestEntry{
			Digest: digest,
			Path:   parts[1],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

// computeDigest computes the SHAKE256 digest of the given content.
func computeDigest(content string) storage.Digest {
	h := sha3.NewShake256()
	h.Write([]byte(content))
	var hashBytes [64]byte
	h.Read(hashBytes[:])
	return storage.Digest{
		Algorithm: "shake256",
		Value:     hashBytes[:],
	}
}

// GetManifest retrieves a manifest by its digest.
func (s *ManifestStore) GetManifest(ctx context.Context, digest storage.Digest) (*storage.Manifest, error) {
	path := s.manifestPath(digest)
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	return ParseManifest(string(content))
}

// PutManifest stores a manifest and returns its computed digest.
func (s *ManifestStore) PutManifest(ctx context.Context, manifest *storage.Manifest) (storage.Digest, error) {
	content := SerializeManifest(manifest)
	digest := computeDigest(content)

	path := s.manifestPath(digest)

	// Check if already exists (deduplication)
	if _, err := os.Stat(path); err == nil {
		return digest, nil
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return storage.Digest{}, err
	}

	// Write atomically via temp file
	tmpFile, err := os.CreateTemp(filepath.Dir(path), "manifest-*.tmp")
	if err != nil {
		return storage.Digest{}, err
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return storage.Digest{}, err
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return storage.Digest{}, err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return storage.Digest{}, err
	}

	return digest, nil
}

// Exists checks if a manifest with the given digest exists.
func (s *ManifestStore) Exists(ctx context.Context, digest storage.Digest) (bool, error) {
	path := s.manifestPath(digest)
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

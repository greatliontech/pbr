package filesystem

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/greatliontech/pbr/internal/storage"
	"golang.org/x/crypto/sha3"
)

// BlobStore implements storage.BlobStore using the filesystem.
// Blobs are stored at: <basePath>/<algorithm>/<first-2-hex>/<full-hex-digest>
type BlobStore struct {
	basePath string
}

// NewBlobStore creates a new filesystem-backed blob store.
func NewBlobStore(basePath string) *BlobStore {
	return &BlobStore{basePath: basePath}
}

// blobPath returns the filesystem path for a given digest.
func (s *BlobStore) blobPath(digest storage.Digest) string {
	hex := digest.Hex()
	if len(hex) < 2 {
		return filepath.Join(s.basePath, digest.Algorithm, hex)
	}
	// Shard by first 2 hex characters to avoid too many files in one directory
	return filepath.Join(s.basePath, digest.Algorithm, hex[:2], hex)
}

// Get retrieves a blob by its digest.
func (s *BlobStore) Get(ctx context.Context, digest storage.Digest) (io.ReadCloser, error) {
	path := s.blobPath(digest)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	return f, nil
}

// Put stores a blob and returns its computed SHAKE256 digest.
func (s *BlobStore) Put(ctx context.Context, r io.Reader) (storage.Digest, error) {
	// Create a temporary file in the base path
	if err := os.MkdirAll(s.basePath, 0755); err != nil {
		return storage.Digest{}, err
	}

	tmpFile, err := os.CreateTemp(s.basePath, "blob-*.tmp")
	if err != nil {
		return storage.Digest{}, err
	}
	tmpPath := tmpFile.Name()
	defer func() {
		// Clean up temp file on error
		if tmpFile != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
		}
	}()

	// Hash while writing
	h := sha3.NewShake256()
	w := io.MultiWriter(tmpFile, h)

	if _, err := io.Copy(w, r); err != nil {
		return storage.Digest{}, err
	}

	if err := tmpFile.Close(); err != nil {
		return storage.Digest{}, err
	}

	// Read the digest
	var hashBytes [64]byte
	h.Read(hashBytes[:])

	digest := storage.Digest{
		Algorithm: "shake256",
		Value:     hashBytes[:],
	}

	// Move to final location
	finalPath := s.blobPath(digest)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0755); err != nil {
		return storage.Digest{}, err
	}

	// Check if blob already exists (deduplication)
	if _, err := os.Stat(finalPath); err == nil {
		// Already exists, remove temp file
		os.Remove(tmpPath)
		tmpFile = nil
		return digest, nil
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return storage.Digest{}, err
	}

	tmpFile = nil // Prevent cleanup
	return digest, nil
}

// Exists checks if a blob with the given digest exists.
func (s *BlobStore) Exists(ctx context.Context, digest storage.Digest) (bool, error) {
	path := s.blobPath(digest)
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// Delete removes a blob by its digest.
func (s *BlobStore) Delete(ctx context.Context, digest storage.Digest) error {
	path := s.blobPath(digest)
	err := os.Remove(path)
	if err != nil && os.IsNotExist(err) {
		return nil // Already deleted
	}
	return err
}

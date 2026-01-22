package storage

import (
	"bytes"
	"context"
	"io"

	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"
	"golang.org/x/crypto/sha3"
)

// BlobStoreImpl implements BlobStore using gocloud.dev/blob.
// Blobs are stored at: <algorithm>/<first-2-hex>/<full-hex-digest>
type BlobStoreImpl struct {
	bucket *blob.Bucket
}

// NewBlobStore creates a new gocloud.dev/blob-backed blob store.
func NewBlobStore(bucket *blob.Bucket) *BlobStoreImpl {
	return &BlobStoreImpl{bucket: bucket}
}

// blobKey returns the key for a given digest.
func blobKey(digest Digest) string {
	hex := digest.Hex()
	if len(hex) < 2 {
		return digest.Algorithm + "/" + hex
	}
	// Shard by first 2 hex characters to avoid too many files in one directory
	return digest.Algorithm + "/" + hex[:2] + "/" + hex
}

// Get retrieves a blob by its digest.
func (s *BlobStoreImpl) Get(ctx context.Context, digest Digest) (io.ReadCloser, error) {
	key := blobKey(digest)
	r, err := s.bucket.NewReader(ctx, key, nil)
	if err != nil {
		if gcerrors.Code(err) == gcerrors.NotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return r, nil
}

// Put stores a blob and returns its computed SHAKE256 digest.
func (s *BlobStoreImpl) Put(ctx context.Context, r io.Reader) (Digest, error) {
	// Read all content to compute hash first
	content, err := io.ReadAll(r)
	if err != nil {
		return Digest{}, err
	}

	// Compute SHAKE256 hash
	h := sha3.NewShake256()
	h.Write(content)
	var hashBytes [64]byte
	h.Read(hashBytes[:])

	digest := Digest{
		Algorithm: DigestAlgorithmShake256,
		Value:     hashBytes[:],
	}

	key := blobKey(digest)

	// Check if blob already exists (deduplication)
	exists, err := s.bucket.Exists(ctx, key)
	if err != nil {
		return Digest{}, err
	}
	if exists {
		return digest, nil
	}

	// Write the blob
	w, err := s.bucket.NewWriter(ctx, key, nil)
	if err != nil {
		return Digest{}, err
	}

	if _, err := io.Copy(w, bytes.NewReader(content)); err != nil {
		w.Close()
		return Digest{}, err
	}

	if err := w.Close(); err != nil {
		return Digest{}, err
	}

	return digest, nil
}

// Exists checks if a blob with the given digest exists.
func (s *BlobStoreImpl) Exists(ctx context.Context, digest Digest) (bool, error) {
	key := blobKey(digest)
	return s.bucket.Exists(ctx, key)
}

// Delete removes a blob by its digest.
func (s *BlobStoreImpl) Delete(ctx context.Context, digest Digest) error {
	key := blobKey(digest)
	err := s.bucket.Delete(ctx, key)
	if err != nil && gcerrors.Code(err) == gcerrors.NotFound {
		return nil // Already deleted
	}
	return err
}

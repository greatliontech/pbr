package storage

import (
	"bytes"
	"context"
	"io"
	"testing"

	"gocloud.dev/blob/memblob"
)

func TestBlobStore_PutGet(t *testing.T) {
	bucket := memblob.OpenBucket(nil)
	defer bucket.Close()

	store := NewBlobStore(bucket)
	ctx := context.Background()

	content := []byte("hello world")
	digest, err := store.Put(ctx, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if digest.Algorithm != "shake256" {
		t.Errorf("expected algorithm shake256, got %s", digest.Algorithm)
	}

	// Get the blob back
	reader, err := store.Get(ctx, digest)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestBlobStore_Exists(t *testing.T) {
	bucket := memblob.OpenBucket(nil)
	defer bucket.Close()

	store := NewBlobStore(bucket)
	ctx := context.Background()

	content := []byte("test content")
	digest, err := store.Put(ctx, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	exists, err := store.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Error("expected blob to exist")
	}

	// Check non-existent blob
	nonExistentDigest := Digest{Algorithm: "shake256", Value: make([]byte, 64)}
	exists, err = store.Exists(ctx, nonExistentDigest)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if exists {
		t.Error("expected blob to not exist")
	}
}

func TestBlobStore_Delete(t *testing.T) {
	bucket := memblob.OpenBucket(nil)
	defer bucket.Close()

	store := NewBlobStore(bucket)
	ctx := context.Background()

	content := []byte("to be deleted")
	digest, err := store.Put(ctx, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Delete the blob
	if err := store.Delete(ctx, digest); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone
	exists, err := store.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if exists {
		t.Error("expected blob to be deleted")
	}
}

func TestBlobStore_GetNotFound(t *testing.T) {
	bucket := memblob.OpenBucket(nil)
	defer bucket.Close()

	store := NewBlobStore(bucket)
	ctx := context.Background()

	nonExistentDigest := Digest{Algorithm: "shake256", Value: make([]byte, 64)}
	_, err := store.Get(ctx, nonExistentDigest)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestBlobStore_Deduplication(t *testing.T) {
	bucket := memblob.OpenBucket(nil)
	defer bucket.Close()

	store := NewBlobStore(bucket)
	ctx := context.Background()

	content := []byte("dedupe test")

	// Put the same content twice
	digest1, err := store.Put(ctx, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("first Put failed: %v", err)
	}

	digest2, err := store.Put(ctx, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("second Put failed: %v", err)
	}

	// Should get the same digest
	if digest1.Hex() != digest2.Hex() {
		t.Errorf("expected same digest, got %s and %s", digest1.Hex(), digest2.Hex())
	}
}

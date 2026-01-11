package filesystem

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/greatliontech/pbr/internal/storage"
)

func TestBlobStore_PutGet(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "blobstore-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewBlobStore(tmpDir)
	ctx := context.Background()

	content := []byte("hello world")

	// Put
	digest, err := store.Put(ctx, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if digest.Algorithm != "shake256" {
		t.Errorf("expected algorithm shake256, got %s", digest.Algorithm)
	}

	if len(digest.Value) != 64 {
		t.Errorf("expected 64 byte hash, got %d", len(digest.Value))
	}

	// Get
	rc, err := store.Get(ctx, digest)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestBlobStore_Exists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "blobstore-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewBlobStore(tmpDir)
	ctx := context.Background()

	content := []byte("test content")

	// Should not exist initially
	fakeDigest := storage.Digest{Algorithm: "shake256", Value: make([]byte, 64)}
	exists, err := store.Exists(ctx, fakeDigest)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if exists {
		t.Error("expected blob to not exist")
	}

	// Put and check exists
	digest, err := store.Put(ctx, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	exists, err = store.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Error("expected blob to exist")
	}
}

func TestBlobStore_Delete(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "blobstore-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewBlobStore(tmpDir)
	ctx := context.Background()

	content := []byte("delete me")

	digest, err := store.Put(ctx, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Delete
	if err := store.Delete(ctx, digest); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Should not exist anymore
	exists, err := store.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if exists {
		t.Error("expected blob to be deleted")
	}

	// Delete again should not error
	if err := store.Delete(ctx, digest); err != nil {
		t.Errorf("Delete of non-existent blob should not error: %v", err)
	}
}

func TestBlobStore_GetNotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "blobstore-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewBlobStore(tmpDir)
	ctx := context.Background()

	fakeDigest := storage.Digest{Algorithm: "shake256", Value: make([]byte, 64)}
	_, err = store.Get(ctx, fakeDigest)
	if err != storage.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestBlobStore_Deduplication(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "blobstore-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewBlobStore(tmpDir)
	ctx := context.Background()

	content := []byte("deduplicate this")

	// Put twice
	digest1, err := store.Put(ctx, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	digest2, err := store.Put(ctx, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Should return same digest
	if digest1.Hex() != digest2.Hex() {
		t.Errorf("expected same digest, got %s and %s", digest1.Hex(), digest2.Hex())
	}
}

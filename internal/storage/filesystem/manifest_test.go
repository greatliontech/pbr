package filesystem

import (
	"context"
	"os"
	"testing"

	"github.com/greatliontech/pbr/internal/storage"
)

func TestManifestStore_PutGet(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "manifeststore-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewManifestStore(tmpDir)
	ctx := context.Background()

	// Create a manifest with some entries
	manifest := &storage.Manifest{
		Entries: []storage.ManifestEntry{
			{
				Digest: storage.Digest{Algorithm: "shake256", Value: make([]byte, 64)},
				Path:   "foo.proto",
			},
			{
				Digest: storage.Digest{Algorithm: "shake256", Value: make([]byte, 64)},
				Path:   "bar.proto",
			},
		},
	}
	// Set some non-zero values in the digest
	manifest.Entries[0].Digest.Value[0] = 0xab
	manifest.Entries[1].Digest.Value[0] = 0xcd

	// Put
	digest, err := store.PutManifest(ctx, manifest)
	if err != nil {
		t.Fatalf("PutManifest failed: %v", err)
	}

	if digest.Algorithm != "shake256" {
		t.Errorf("expected algorithm shake256, got %s", digest.Algorithm)
	}

	// Get
	got, err := store.GetManifest(ctx, digest)
	if err != nil {
		t.Fatalf("GetManifest failed: %v", err)
	}

	// Entries should be sorted by path
	if len(got.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got.Entries))
	}

	// After sorting: bar.proto, foo.proto
	if got.Entries[0].Path != "bar.proto" {
		t.Errorf("expected first entry to be bar.proto, got %s", got.Entries[0].Path)
	}
	if got.Entries[1].Path != "foo.proto" {
		t.Errorf("expected second entry to be foo.proto, got %s", got.Entries[1].Path)
	}
}

func TestManifestStore_Exists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "manifeststore-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewManifestStore(tmpDir)
	ctx := context.Background()

	// Should not exist initially
	fakeDigest := storage.Digest{Algorithm: "shake256", Value: make([]byte, 64)}
	exists, err := store.Exists(ctx, fakeDigest)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if exists {
		t.Error("expected manifest to not exist")
	}

	// Put and check exists
	manifest := &storage.Manifest{
		Entries: []storage.ManifestEntry{
			{Digest: storage.Digest{Algorithm: "shake256", Value: make([]byte, 64)}, Path: "test.proto"},
		},
	}

	digest, err := store.PutManifest(ctx, manifest)
	if err != nil {
		t.Fatalf("PutManifest failed: %v", err)
	}

	exists, err = store.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Error("expected manifest to exist")
	}
}

func TestManifestStore_GetNotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "manifeststore-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewManifestStore(tmpDir)
	ctx := context.Background()

	fakeDigest := storage.Digest{Algorithm: "shake256", Value: make([]byte, 64)}
	_, err = store.GetManifest(ctx, fakeDigest)
	if err != storage.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestSerializeManifest(t *testing.T) {
	digest1 := storage.Digest{Algorithm: "shake256", Value: make([]byte, 64)}
	digest1.Value[0] = 0xaa
	digest2 := storage.Digest{Algorithm: "shake256", Value: make([]byte, 64)}
	digest2.Value[0] = 0xbb

	manifest := &storage.Manifest{
		Entries: []storage.ManifestEntry{
			{Digest: digest1, Path: "z.proto"},
			{Digest: digest2, Path: "a.proto"},
		},
	}

	content := SerializeManifest(manifest)

	// Should be sorted by path
	expected := "shake256:" + digest2.Hex() + "  a.proto\n" +
		"shake256:" + digest1.Hex() + "  z.proto\n"

	if content != expected {
		t.Errorf("unexpected serialization:\ngot:\n%s\nwant:\n%s", content, expected)
	}
}

func TestParseManifest(t *testing.T) {
	content := "shake256:aa00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000  foo.proto\n" +
		"shake256:bb00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000  bar.proto\n"

	manifest, err := ParseManifest(content)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	if len(manifest.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(manifest.Entries))
	}

	if manifest.Entries[0].Path != "foo.proto" {
		t.Errorf("expected foo.proto, got %s", manifest.Entries[0].Path)
	}
	if manifest.Entries[0].Digest.Value[0] != 0xaa {
		t.Errorf("expected 0xaa, got 0x%x", manifest.Entries[0].Digest.Value[0])
	}
}

func TestManifestStore_Deduplication(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "manifeststore-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewManifestStore(tmpDir)
	ctx := context.Background()

	manifest := &storage.Manifest{
		Entries: []storage.ManifestEntry{
			{Digest: storage.Digest{Algorithm: "shake256", Value: make([]byte, 64)}, Path: "test.proto"},
		},
	}

	// Put twice
	digest1, err := store.PutManifest(ctx, manifest)
	if err != nil {
		t.Fatalf("PutManifest failed: %v", err)
	}

	digest2, err := store.PutManifest(ctx, manifest)
	if err != nil {
		t.Fatalf("PutManifest failed: %v", err)
	}

	// Should return same digest
	if digest1.Hex() != digest2.Hex() {
		t.Errorf("expected same digest, got %s and %s", digest1.Hex(), digest2.Hex())
	}
}

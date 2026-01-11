package cas

import (
	"context"
	"os"
	"testing"

	"github.com/greatliontech/pbr/internal/storage/filesystem"
)

func setupTestRegistry(t *testing.T) (*Registry, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "cas-registry-test-*")
	if err != nil {
		t.Fatal(err)
	}

	blobStore := filesystem.NewBlobStore(tmpDir + "/blobs")
	manifestStore := filesystem.NewManifestStore(tmpDir + "/manifests")
	metadataStore := filesystem.NewMetadataStore(tmpDir + "/metadata")

	reg := New(blobStore, manifestStore, metadataStore, "test.registry.com")

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return reg, cleanup
}

func TestRegistry_CreateModule(t *testing.T) {
	reg, cleanup := setupTestRegistry(t)
	defer cleanup()

	ctx := context.Background()

	// Create module
	mod, err := reg.CreateModule(ctx, "testowner", "testmodule", "Test module description")
	if err != nil {
		t.Fatalf("CreateModule failed: %v", err)
	}

	if mod.Owner() != "testowner" {
		t.Errorf("expected owner 'testowner', got %s", mod.Owner())
	}
	if mod.Name() != "testmodule" {
		t.Errorf("expected name 'testmodule', got %s", mod.Name())
	}

	// Get module
	got, err := reg.Module(ctx, "testowner", "testmodule")
	if err != nil {
		t.Fatalf("Module failed: %v", err)
	}
	if got.ID() != mod.ID() {
		t.Errorf("expected id %s, got %s", mod.ID(), got.ID())
	}

	// Create same module again should return existing
	mod2, err := reg.CreateModule(ctx, "testowner", "testmodule", "Different description")
	if err != nil {
		t.Fatalf("CreateModule failed: %v", err)
	}
	if mod2.ID() != mod.ID() {
		t.Errorf("expected same module id")
	}
}

func TestRegistry_CreateCommit(t *testing.T) {
	reg, cleanup := setupTestRegistry(t)
	defer cleanup()

	ctx := context.Background()

	// Create module
	mod, err := reg.CreateModule(ctx, "testowner", "testmodule", "")
	if err != nil {
		t.Fatalf("CreateModule failed: %v", err)
	}

	// Create commit with files
	files := []File{
		{Path: "test.proto", Content: "syntax = \"proto3\";\npackage test;"},
		{Path: "buf.yaml", Content: "version: v1\nname: buf.build/testowner/testmodule"},
	}

	commit, err := mod.CreateCommit(ctx, files, []string{"main"}, "")
	if err != nil {
		t.Fatalf("CreateCommit failed: %v", err)
	}

	if commit.ModuleID != mod.ID() {
		t.Errorf("expected moduleID %s, got %s", mod.ID(), commit.ModuleID)
	}

	// Verify commit ID is 32 chars
	if len(commit.ID) != 32 {
		t.Errorf("expected commit ID length 32, got %d", len(commit.ID))
	}

	// Get commit by ID
	gotCommit, err := mod.CommitByID(ctx, commit.ID)
	if err != nil {
		t.Fatalf("CommitByID failed: %v", err)
	}
	if gotCommit.ID != commit.ID {
		t.Errorf("expected commit ID %s, got %s", commit.ID, gotCommit.ID)
	}

	// Get commit by label
	gotCommit, err = mod.Commit(ctx, "main")
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if gotCommit.ID != commit.ID {
		t.Errorf("expected commit ID %s, got %s", commit.ID, gotCommit.ID)
	}

	// Get files
	gotFiles, _, err := mod.FilesAndCommitByCommitID(ctx, commit.ID)
	if err != nil {
		t.Fatalf("FilesAndCommitByCommitID failed: %v", err)
	}
	if len(gotFiles) != 2 {
		t.Errorf("expected 2 files, got %d", len(gotFiles))
	}
}

func TestRegistry_ModuleByCommitID(t *testing.T) {
	reg, cleanup := setupTestRegistry(t)
	defer cleanup()

	ctx := context.Background()

	// Create module and commit
	mod, err := reg.CreateModule(ctx, "testowner", "testmodule", "")
	if err != nil {
		t.Fatalf("CreateModule failed: %v", err)
	}

	files := []File{
		{Path: "test.proto", Content: "syntax = \"proto3\";"},
	}

	commit, err := mod.CreateCommit(ctx, files, []string{"main"}, "")
	if err != nil {
		t.Fatalf("CreateCommit failed: %v", err)
	}

	// Find module by commit ID
	gotMod, err := reg.ModuleByCommitID(ctx, commit.ID)
	if err != nil {
		t.Fatalf("ModuleByCommitID failed: %v", err)
	}
	if gotMod.ID() != mod.ID() {
		t.Errorf("expected module ID %s, got %s", mod.ID(), gotMod.ID())
	}
}

func TestRegistry_CommitDeduplication(t *testing.T) {
	reg, cleanup := setupTestRegistry(t)
	defer cleanup()

	ctx := context.Background()

	mod, err := reg.CreateModule(ctx, "testowner", "testmodule", "")
	if err != nil {
		t.Fatalf("CreateModule failed: %v", err)
	}

	files := []File{
		{Path: "test.proto", Content: "syntax = \"proto3\";"},
	}

	// Create same content twice
	commit1, err := mod.CreateCommit(ctx, files, []string{"main"}, "")
	if err != nil {
		t.Fatalf("CreateCommit failed: %v", err)
	}

	commit2, err := mod.CreateCommit(ctx, files, []string{"v1.0.0"}, "")
	if err != nil {
		t.Fatalf("CreateCommit failed: %v", err)
	}

	// Should return same commit ID (content-addressed)
	if commit1.ID != commit2.ID {
		t.Errorf("expected same commit ID for identical content, got %s and %s", commit1.ID, commit2.ID)
	}

	// Both labels should point to the same commit
	gotMain, err := mod.Commit(ctx, "main")
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	gotV1, err := mod.Commit(ctx, "v1.0.0")
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	if gotMain.ID != gotV1.ID {
		t.Errorf("expected same commit for both labels")
	}
}

func TestParseBufLock(t *testing.T) {
	content := `version: v1
deps:
  - remote: buf.build
    owner: googleapis
    repository: googleapis
    commit: cc916c31859748a68fd229a3c8d7a2e8
    digest: shake256:469b049d0f58c6eedc4f3ae52e5b4395a99d6417e0d5a3cdd04b400dc4b3e4f41d7ce326a96c1d1c955a7fdf61a3e6b0c31a3da9692d5d72e4e50a30a9e16f1
  - remote: buf.build
    owner: grpc-ecosystem
    repository: grpc-gateway
    commit: bc28b723cd7746fa80d15cbc63c8b5e8
    digest: shake256:8b1d7f88bfc34b79a68e82c3c0a23f1842fbcbf8c7d4de0b31d2a6e5c7a0f9e3d7c1f5e2b8a9d4c0e7f1a3b6c9d2e5f8a1b4c7d0e3f6a9b2c5d8e1f4a7b0c3d6e9
`

	lock, err := ParseBufLock(content)
	if err != nil {
		t.Fatalf("ParseBufLock failed: %v", err)
	}

	if len(lock.Deps) != 2 {
		t.Errorf("expected 2 deps, got %d", len(lock.Deps))
	}

	if lock.Deps[0].Remote != "buf.build" {
		t.Errorf("expected remote 'buf.build', got %s", lock.Deps[0].Remote)
	}

	if lock.Deps[0].Owner != "googleapis" {
		t.Errorf("expected owner 'googleapis', got %s", lock.Deps[0].Owner)
	}

	if lock.Deps[0].Repository != "googleapis" {
		t.Errorf("expected repository 'googleapis', got %s", lock.Deps[0].Repository)
	}
}

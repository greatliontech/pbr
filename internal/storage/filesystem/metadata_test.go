package filesystem

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/greatliontech/pbr/internal/storage"
)

func TestMetadataStore_Owner(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "metadatastore-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewMetadataStore(tmpDir)
	ctx := context.Background()

	owner := &storage.OwnerRecord{
		ID:         "owner123",
		Name:       "testowner",
		CreateTime: time.Now(),
	}

	// Create
	if err := store.CreateOwner(ctx, owner); err != nil {
		t.Fatalf("CreateOwner failed: %v", err)
	}

	// Create again should fail
	if err := store.CreateOwner(ctx, owner); err != storage.ErrAlreadyExists {
		t.Errorf("expected ErrAlreadyExists, got %v", err)
	}

	// Get by ID
	got, err := store.GetOwner(ctx, owner.ID)
	if err != nil {
		t.Fatalf("GetOwner failed: %v", err)
	}
	if got.Name != owner.Name {
		t.Errorf("expected name %s, got %s", owner.Name, got.Name)
	}

	// Get by name
	got, err = store.GetOwnerByName(ctx, owner.Name)
	if err != nil {
		t.Fatalf("GetOwnerByName failed: %v", err)
	}
	if got.ID != owner.ID {
		t.Errorf("expected id %s, got %s", owner.ID, got.ID)
	}

	// List
	owners, err := store.ListOwners(ctx)
	if err != nil {
		t.Fatalf("ListOwners failed: %v", err)
	}
	if len(owners) != 1 {
		t.Errorf("expected 1 owner, got %d", len(owners))
	}
}

func TestMetadataStore_Module(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "metadatastore-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewMetadataStore(tmpDir)
	ctx := context.Background()

	module := &storage.ModuleRecord{
		ID:               "module123",
		OwnerID:          "owner123",
		Owner:            "testowner",
		Name:             "testmodule",
		DefaultLabelName: "main",
		CreateTime:       time.Now(),
		UpdateTime:       time.Now(),
	}

	// Create
	if err := store.CreateModule(ctx, module); err != nil {
		t.Fatalf("CreateModule failed: %v", err)
	}

	// Get by ID
	got, err := store.GetModule(ctx, module.ID)
	if err != nil {
		t.Fatalf("GetModule failed: %v", err)
	}
	if got.Name != module.Name {
		t.Errorf("expected name %s, got %s", module.Name, got.Name)
	}

	// Get by name
	got, err = store.GetModuleByName(ctx, module.Owner, module.Name)
	if err != nil {
		t.Fatalf("GetModuleByName failed: %v", err)
	}
	if got.ID != module.ID {
		t.Errorf("expected id %s, got %s", module.ID, got.ID)
	}

	// Update
	module.Description = "updated description"
	if err := store.UpdateModule(ctx, module); err != nil {
		t.Fatalf("UpdateModule failed: %v", err)
	}

	got, err = store.GetModule(ctx, module.ID)
	if err != nil {
		t.Fatalf("GetModule failed: %v", err)
	}
	if got.Description != "updated description" {
		t.Errorf("expected description 'updated description', got %s", got.Description)
	}

	// List by owner
	modules, err := store.ListModules(ctx, module.OwnerID)
	if err != nil {
		t.Fatalf("ListModules failed: %v", err)
	}
	if len(modules) != 1 {
		t.Errorf("expected 1 module, got %d", len(modules))
	}

	// Delete
	if err := store.DeleteModule(ctx, module.ID); err != nil {
		t.Fatalf("DeleteModule failed: %v", err)
	}

	_, err = store.GetModule(ctx, module.ID)
	if err != storage.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestMetadataStore_Commit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "metadatastore-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewMetadataStore(tmpDir)
	ctx := context.Background()

	digest := storage.Digest{Algorithm: "shake256", Value: make([]byte, 64)}
	digest.Value[0] = 0xab

	commit := &storage.CommitRecord{
		ID:             "commit123456789012345678901234",
		ModuleID:       "module123",
		OwnerID:        "owner123",
		ManifestDigest: digest,
		CreateTime:     time.Now(),
	}

	// Create
	if err := store.CreateCommit(ctx, commit); err != nil {
		t.Fatalf("CreateCommit failed: %v", err)
	}

	// Create again should be idempotent
	if err := store.CreateCommit(ctx, commit); err != nil {
		t.Errorf("expected idempotent create, got %v", err)
	}

	// Get by ID
	got, err := store.GetCommit(ctx, commit.ID)
	if err != nil {
		t.Fatalf("GetCommit failed: %v", err)
	}
	if got.ModuleID != commit.ModuleID {
		t.Errorf("expected moduleID %s, got %s", commit.ModuleID, got.ModuleID)
	}

	// Get by digest
	got, err = store.GetCommitByDigest(ctx, digest)
	if err != nil {
		t.Fatalf("GetCommitByDigest failed: %v", err)
	}
	if got.ID != commit.ID {
		t.Errorf("expected id %s, got %s", commit.ID, got.ID)
	}

	// List commits
	commits, nextToken, err := store.ListCommits(ctx, commit.ModuleID, 10, "")
	if err != nil {
		t.Fatalf("ListCommits failed: %v", err)
	}
	if len(commits) != 1 {
		t.Errorf("expected 1 commit, got %d", len(commits))
	}
	if nextToken != "" {
		t.Errorf("expected empty next token, got %s", nextToken)
	}
}

func TestMetadataStore_Label(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "metadatastore-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewMetadataStore(tmpDir)
	ctx := context.Background()

	label := &storage.LabelRecord{
		ID:       "label123",
		ModuleID: "module123",
		Name:     "main",
		CommitID: "commit123",
	}

	// Create
	if err := store.CreateOrUpdateLabel(ctx, label); err != nil {
		t.Fatalf("CreateOrUpdateLabel failed: %v", err)
	}

	// Get
	got, err := store.GetLabel(ctx, label.ModuleID, label.Name)
	if err != nil {
		t.Fatalf("GetLabel failed: %v", err)
	}
	if got.CommitID != label.CommitID {
		t.Errorf("expected commitID %s, got %s", label.CommitID, got.CommitID)
	}

	// Update
	label.CommitID = "commit456"
	if err := store.CreateOrUpdateLabel(ctx, label); err != nil {
		t.Fatalf("CreateOrUpdateLabel failed: %v", err)
	}

	got, err = store.GetLabel(ctx, label.ModuleID, label.Name)
	if err != nil {
		t.Fatalf("GetLabel failed: %v", err)
	}
	if got.CommitID != "commit456" {
		t.Errorf("expected commitID commit456, got %s", got.CommitID)
	}

	// List
	labels, err := store.ListLabels(ctx, label.ModuleID)
	if err != nil {
		t.Fatalf("ListLabels failed: %v", err)
	}
	if len(labels) != 1 {
		t.Errorf("expected 1 label, got %d", len(labels))
	}

	// Delete
	if err := store.DeleteLabel(ctx, label.ModuleID, label.Name); err != nil {
		t.Fatalf("DeleteLabel failed: %v", err)
	}

	_, err = store.GetLabel(ctx, label.ModuleID, label.Name)
	if err != storage.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

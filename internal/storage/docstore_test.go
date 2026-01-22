package storage

import (
	"context"
	"testing"
	"time"

	"gocloud.dev/docstore/memdocstore"
)

func setupTestMetadataStore(t *testing.T) *MetadataStoreImpl {
	owners, err := memdocstore.OpenCollection("ID", nil)
	if err != nil {
		t.Fatalf("failed to open owners collection: %v", err)
	}
	modules, err := memdocstore.OpenCollection("ID", nil)
	if err != nil {
		t.Fatalf("failed to open modules collection: %v", err)
	}
	commits, err := memdocstore.OpenCollection("ID", nil)
	if err != nil {
		t.Fatalf("failed to open commits collection: %v", err)
	}
	labels, err := memdocstore.OpenCollection("ID", nil)
	if err != nil {
		t.Fatalf("failed to open labels collection: %v", err)
	}
	return NewMetadataStore(owners, modules, commits, labels)
}

func TestMetadataStore_Owner(t *testing.T) {
	store := setupTestMetadataStore(t)
	ctx := context.Background()

	owner := &OwnerRecord{
		ID:         "owner-123",
		Name:       "testowner",
		CreateTime: time.Now().UTC(),
	}

	// Create owner
	if err := store.CreateOwner(ctx, owner); err != nil {
		t.Fatalf("CreateOwner failed: %v", err)
	}

	// Get by ID
	got, err := store.GetOwner(ctx, owner.ID)
	if err != nil {
		t.Fatalf("GetOwner failed: %v", err)
	}
	if got.Name != owner.Name {
		t.Errorf("expected name %q, got %q", owner.Name, got.Name)
	}

	// Get by name
	got, err = store.GetOwnerByName(ctx, owner.Name)
	if err != nil {
		t.Fatalf("GetOwnerByName failed: %v", err)
	}
	if got.ID != owner.ID {
		t.Errorf("expected ID %q, got %q", owner.ID, got.ID)
	}

	// List owners
	owners, err := store.ListOwners(ctx)
	if err != nil {
		t.Fatalf("ListOwners failed: %v", err)
	}
	if len(owners) != 1 {
		t.Errorf("expected 1 owner, got %d", len(owners))
	}
}

func TestMetadataStore_Module(t *testing.T) {
	store := setupTestMetadataStore(t)
	ctx := context.Background()

	module := &ModuleRecord{
		ID:               "module-123",
		OwnerID:          "owner-123",
		Owner:            "testowner",
		Name:             "testmodule",
		DefaultLabelName: "main",
		CreateTime:       time.Now().UTC(),
		UpdateTime:       time.Now().UTC(),
	}

	// Create module
	if err := store.CreateModule(ctx, module); err != nil {
		t.Fatalf("CreateModule failed: %v", err)
	}

	// Get by ID
	got, err := store.GetModule(ctx, module.ID)
	if err != nil {
		t.Fatalf("GetModule failed: %v", err)
	}
	if got.Name != module.Name {
		t.Errorf("expected name %q, got %q", module.Name, got.Name)
	}

	// Get by name
	got, err = store.GetModuleByName(ctx, module.Owner, module.Name)
	if err != nil {
		t.Fatalf("GetModuleByName failed: %v", err)
	}
	if got.ID != module.ID {
		t.Errorf("expected ID %q, got %q", module.ID, got.ID)
	}

	// Update module
	module.Description = "updated description"
	if err := store.UpdateModule(ctx, module); err != nil {
		t.Fatalf("UpdateModule failed: %v", err)
	}
	got, _ = store.GetModule(ctx, module.ID)
	if got.Description != "updated description" {
		t.Errorf("expected description %q, got %q", "updated description", got.Description)
	}

	// Delete module
	if err := store.DeleteModule(ctx, module.ID); err != nil {
		t.Fatalf("DeleteModule failed: %v", err)
	}
	_, err = store.GetModule(ctx, module.ID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMetadataStore_Commit(t *testing.T) {
	store := setupTestMetadataStore(t)
	ctx := context.Background()

	digest := Digest{
		Algorithm: "shake256",
		Value:     make([]byte, 64),
	}
	// Set a recognizable pattern
	for i := range digest.Value {
		digest.Value[i] = byte(i)
	}

	b5Digest := ModuleDigest{
		Type:  DigestTypeB5,
		Value: make([]byte, 64),
	}
	// Set a different recognizable pattern
	for i := range b5Digest.Value {
		b5Digest.Value[i] = byte(i + 100)
	}

	commit := &CommitRecord{
		ID:             "commit-123",
		ModuleID:       "module-123",
		OwnerID:        "owner-123",
		ManifestDigest: digest,
		B5Digest:       b5Digest,
		CreateTime:     time.Now().UTC(),
	}

	// Create commit
	if err := store.CreateCommit(ctx, commit); err != nil {
		t.Fatalf("CreateCommit failed: %v", err)
	}

	// Get by ID
	got, err := store.GetCommit(ctx, commit.ID)
	if err != nil {
		t.Fatalf("GetCommit failed: %v", err)
	}
	if got.ModuleID != commit.ModuleID {
		t.Errorf("expected module ID %q, got %q", commit.ModuleID, got.ModuleID)
	}

	// Get by digest
	got, err = store.GetCommitByDigest(ctx, digest)
	if err != nil {
		t.Fatalf("GetCommitByDigest failed: %v", err)
	}
	if got.ID != commit.ID {
		t.Errorf("expected ID %q, got %q", commit.ID, got.ID)
	}
}

func TestMetadataStore_Label(t *testing.T) {
	store := setupTestMetadataStore(t)
	ctx := context.Background()

	label := &LabelRecord{
		ModuleID: "module-123",
		Name:     "main",
		CommitID: "commit-123",
	}

	// Create label
	if err := store.CreateOrUpdateLabel(ctx, label); err != nil {
		t.Fatalf("CreateOrUpdateLabel failed: %v", err)
	}

	// Get label
	got, err := store.GetLabel(ctx, label.ModuleID, label.Name)
	if err != nil {
		t.Fatalf("GetLabel failed: %v", err)
	}
	if got.CommitID != label.CommitID {
		t.Errorf("expected commit ID %q, got %q", label.CommitID, got.CommitID)
	}

	// Update label
	label.CommitID = "commit-456"
	if err := store.CreateOrUpdateLabel(ctx, label); err != nil {
		t.Fatalf("CreateOrUpdateLabel (update) failed: %v", err)
	}
	got, _ = store.GetLabel(ctx, label.ModuleID, label.Name)
	if got.CommitID != "commit-456" {
		t.Errorf("expected commit ID %q, got %q", "commit-456", got.CommitID)
	}

	// List labels
	labels, err := store.ListLabels(ctx, label.ModuleID)
	if err != nil {
		t.Fatalf("ListLabels failed: %v", err)
	}
	if len(labels) != 1 {
		t.Errorf("expected 1 label, got %d", len(labels))
	}

	// Delete label
	if err := store.DeleteLabel(ctx, label.ModuleID, label.Name); err != nil {
		t.Fatalf("DeleteLabel failed: %v", err)
	}
	_, err = store.GetLabel(ctx, label.ModuleID, label.Name)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMetadataStore_OwnerNotFound(t *testing.T) {
	store := setupTestMetadataStore(t)
	ctx := context.Background()

	_, err := store.GetOwner(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	_, err = store.GetOwnerByName(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMetadataStore_ModuleNotFound(t *testing.T) {
	store := setupTestMetadataStore(t)
	ctx := context.Background()

	_, err := store.GetModule(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	_, err = store.GetModuleByName(ctx, "owner", "nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMetadataStore_CommitNotFound(t *testing.T) {
	store := setupTestMetadataStore(t)
	ctx := context.Background()

	_, err := store.GetCommit(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMetadataStore_LabelNotFound(t *testing.T) {
	store := setupTestMetadataStore(t)
	ctx := context.Background()

	_, err := store.GetLabel(ctx, "module", "nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

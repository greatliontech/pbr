package storage

import (
	"context"
	"errors"
	"time"
)

// Common errors for storage operations.
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
)

// ModuleRecord represents stored module metadata.
type ModuleRecord struct {
	ID               string
	OwnerID          string
	Owner            string // owner name
	Name             string
	Description      string
	DefaultLabelName string // e.g., "main"
	CreateTime       time.Time
	UpdateTime       time.Time
}

// CommitRecord represents stored commit metadata.
type CommitRecord struct {
	ID               string
	ModuleID         string
	OwnerID          string
	FilesDigest      Digest       // SHAKE256 digest of the files manifest
	ModuleDigest     ModuleDigest // module digest (B4 or B5)
	CreateTime       time.Time
	CreatedByUserID  string
	SourceControlURL string
	DepCommitIDs     []string // dependency commit IDs
}

// LabelRecord represents a named reference to a commit (like a branch or tag).
type LabelRecord struct {
	ID       string // derived from moduleID + name
	ModuleID string
	Name     string // e.g., "main", "v1.0.0"
	CommitID string
}

// OwnerRecord represents an owner/organization.
type OwnerRecord struct {
	ID         string
	Name       string
	CreateTime time.Time
}

// MetadataStore manages module, commit, label, and owner metadata.
type MetadataStore interface {
	// Owner operations
	GetOwner(ctx context.Context, id string) (*OwnerRecord, error)
	GetOwnerByName(ctx context.Context, name string) (*OwnerRecord, error)
	CreateOwner(ctx context.Context, owner *OwnerRecord) error
	ListOwners(ctx context.Context) ([]*OwnerRecord, error)

	// Module operations
	GetModule(ctx context.Context, id string) (*ModuleRecord, error)
	GetModuleByName(ctx context.Context, owner, name string) (*ModuleRecord, error)
	ListModules(ctx context.Context, ownerID string) ([]*ModuleRecord, error)
	CreateModule(ctx context.Context, module *ModuleRecord) error
	UpdateModule(ctx context.Context, module *ModuleRecord) error
	DeleteModule(ctx context.Context, id string) error

	// Commit operations
	GetCommit(ctx context.Context, id string) (*CommitRecord, error)
	GetCommitByFilesDigest(ctx context.Context, digest Digest) (*CommitRecord, error)
	ListCommits(ctx context.Context, moduleID string, limit int, pageToken string) ([]*CommitRecord, string, error)
	CreateCommit(ctx context.Context, commit *CommitRecord) error

	// Label operations
	GetLabel(ctx context.Context, moduleID, name string) (*LabelRecord, error)
	ListLabels(ctx context.Context, moduleID string) ([]*LabelRecord, error)
	CreateOrUpdateLabel(ctx context.Context, label *LabelRecord) error
	DeleteLabel(ctx context.Context, moduleID, name string) error
}

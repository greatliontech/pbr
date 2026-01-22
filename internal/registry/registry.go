package registry

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/greatliontech/pbr/internal/storage"
	"github.com/greatliontech/pbr/internal/util"
)

// Registry implements a buf-compatible registry using CAS storage.
type Registry struct {
	blobs     storage.BlobStore
	manifests storage.ManifestStore
	metadata  storage.MetadataStore
	hostName  string
}

// New creates a new CAS-backed registry.
func New(
	blobs storage.BlobStore,
	manifests storage.ManifestStore,
	metadata storage.MetadataStore,
	hostName string,
) *Registry {
	return &Registry{
		blobs:     blobs,
		manifests: manifests,
		metadata:  metadata,
		hostName:  hostName,
	}
}

// HostName returns the registry's hostname.
func (r *Registry) HostName() string {
	return r.hostName
}

// Module retrieves a module by owner and name.
func (r *Registry) Module(ctx context.Context, owner, name string) (*Module, error) {
	slog.DebugContext(ctx, "Registry.Module", "owner", owner, "name", name)

	record, err := r.metadata.GetModuleByName(ctx, owner, name)
	if err != nil {
		return nil, err
	}

	return &Module{
		record:   record,
		registry: r,
	}, nil
}

// ModuleByID retrieves a module by its ID.
func (r *Registry) ModuleByID(ctx context.Context, id string) (*Module, error) {
	slog.DebugContext(ctx, "Registry.ModuleByID", "id", id)

	record, err := r.metadata.GetModule(ctx, id)
	if err != nil {
		return nil, err
	}

	return &Module{
		record:   record,
		registry: r,
	}, nil
}

// ModuleByCommitID finds the module that contains a given commit ID.
func (r *Registry) ModuleByCommitID(ctx context.Context, commitID string) (*Module, error) {
	slog.DebugContext(ctx, "Registry.ModuleByCommitID", "commitID", commitID)

	commit, err := r.metadata.GetCommit(ctx, commitID)
	if err != nil {
		return nil, err
	}

	return r.ModuleByID(ctx, commit.ModuleID)
}

// CommitByID retrieves a commit by its ID (from any module).
func (r *Registry) CommitByID(ctx context.Context, commitID string) (*Commit, error) {
	slog.DebugContext(ctx, "Registry.CommitByID", "commitID", commitID)

	record, err := r.metadata.GetCommit(ctx, commitID)
	if err != nil {
		return nil, err
	}

	return &Commit{
		ID:           record.ID,
		ModuleID:     record.ModuleID,
		OwnerID:      record.OwnerID,
		FilesDigest:  record.FilesDigest,
		ModuleDigest: record.ModuleDigest,
		CreateTime:   record.CreateTime,
		DepCommitIDs: record.DepCommitIDs,
	}, nil
}

// GetDepModuleDigests retrieves module digests for a list of dependency commit IDs.
func (r *Registry) GetDepModuleDigests(ctx context.Context, depCommitIDs []string) ([]storage.ModuleDigest, error) {
	digests := make([]storage.ModuleDigest, 0, len(depCommitIDs))
	for _, commitID := range depCommitIDs {
		commit, err := r.CommitByID(ctx, commitID)
		if err != nil {
			return nil, fmt.Errorf("failed to get dependency commit %s: %w", commitID, err)
		}
		digests = append(digests, commit.ModuleDigest)
	}
	return digests, nil
}

// CreateModule creates a new module.
func (r *Registry) CreateModule(ctx context.Context, owner, name, description string) (*Module, error) {
	slog.DebugContext(ctx, "Registry.CreateModule", "owner", owner, "name", name)

	// Get or create owner
	ownerID := util.OwnerID(owner)
	_, err := r.metadata.GetOwner(ctx, ownerID)
	if err == storage.ErrNotFound {
		// Create owner
		ownerRecord := &storage.OwnerRecord{
			ID:         ownerID,
			Name:       owner,
			CreateTime: time.Now(),
		}
		if err := r.metadata.CreateOwner(ctx, ownerRecord); err != nil && err != storage.ErrAlreadyExists {
			return nil, fmt.Errorf("failed to create owner: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to get owner: %w", err)
	}

	moduleID := util.ModuleID(ownerID, name)
	now := time.Now()

	record := &storage.ModuleRecord{
		ID:               moduleID,
		OwnerID:          ownerID,
		Owner:            owner,
		Name:             name,
		Description:      description,
		DefaultLabelName: "main",
		CreateTime:       now,
		UpdateTime:       now,
	}

	if err := r.metadata.CreateModule(ctx, record); err != nil {
		if err == storage.ErrAlreadyExists {
			// Return existing module
			return r.Module(ctx, owner, name)
		}
		return nil, fmt.Errorf("failed to create module: %w", err)
	}

	return &Module{
		record:   record,
		registry: r,
	}, nil
}

// GetOrCreateModule gets an existing module or creates it if it doesn't exist.
func (r *Registry) GetOrCreateModule(ctx context.Context, owner, name string) (*Module, error) {
	mod, err := r.Module(ctx, owner, name)
	if err == nil {
		return mod, nil
	}
	if err != storage.ErrNotFound {
		return nil, err
	}
	return r.CreateModule(ctx, owner, name, "")
}

// DeleteModule deletes a module by owner and name.
func (r *Registry) DeleteModule(ctx context.Context, owner, name string) error {
	slog.DebugContext(ctx, "Registry.DeleteModule", "owner", owner, "name", name)

	record, err := r.metadata.GetModuleByName(ctx, owner, name)
	if err != nil {
		return err
	}

	return r.metadata.DeleteModule(ctx, record.ID)
}

// ListModules lists all modules for an owner.
func (r *Registry) ListModules(ctx context.Context, owner string) ([]*Module, error) {
	slog.DebugContext(ctx, "Registry.ListModules", "owner", owner)

	ownerID := util.OwnerID(owner)
	records, err := r.metadata.ListModules(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	modules := make([]*Module, len(records))
	for i, record := range records {
		modules[i] = &Module{
			record:   record,
			registry: r,
		}
	}
	return modules, nil
}

// Owner retrieves an owner by ID.
func (r *Registry) Owner(ctx context.Context, id string) (*storage.OwnerRecord, error) {
	return r.metadata.GetOwner(ctx, id)
}

// OwnerByName retrieves an owner by name.
func (r *Registry) OwnerByName(ctx context.Context, name string) (*storage.OwnerRecord, error) {
	return r.metadata.GetOwnerByName(ctx, name)
}

// ListOwners lists all owners.
func (r *Registry) ListOwners(ctx context.Context) ([]*storage.OwnerRecord, error) {
	return r.metadata.ListOwners(ctx)
}

// File represents a file in a module.
type File struct {
	Path    string
	Content string
	Digest  storage.Digest
}

// Commit represents a commit in the registry.
type Commit struct {
	ID           string
	ModuleID     string
	OwnerID      string
	FilesDigest  storage.Digest       // SHAKE256 digest of the files manifest
	ModuleDigest storage.ModuleDigest // module digest (B5)
	CreateTime   time.Time
	DepCommitIDs []string // dependency commit IDs
}

// BufLock represents a parsed buf.lock file.
type BufLock struct {
	Deps []BufLockDep
}

// BufLockDep represents a dependency in buf.lock.
type BufLockDep struct {
	Remote     string
	Owner      string
	Repository string
	Commit     string
	Digest     string
}

// ParseBufLock parses buf.lock content.
func ParseBufLock(content string) (*BufLock, error) {
	// Simple YAML-like parsing for buf.lock format
	lock := &BufLock{}
	lines := strings.Split(content, "\n")
	var currentDep *BufLockDep

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if line == "deps:" {
			continue
		}

		if strings.HasPrefix(line, "- remote:") {
			if currentDep != nil {
				lock.Deps = append(lock.Deps, *currentDep)
			}
			currentDep = &BufLockDep{
				Remote: strings.TrimSpace(strings.TrimPrefix(line, "- remote:")),
			}
		} else if currentDep != nil {
			if strings.HasPrefix(line, "owner:") {
				currentDep.Owner = strings.TrimSpace(strings.TrimPrefix(line, "owner:"))
			} else if strings.HasPrefix(line, "repository:") {
				currentDep.Repository = strings.TrimSpace(strings.TrimPrefix(line, "repository:"))
			} else if strings.HasPrefix(line, "commit:") {
				currentDep.Commit = strings.TrimSpace(strings.TrimPrefix(line, "commit:"))
			} else if strings.HasPrefix(line, "digest:") {
				currentDep.Digest = strings.TrimSpace(strings.TrimPrefix(line, "digest:"))
			}
		}
	}

	if currentDep != nil {
		lock.Deps = append(lock.Deps, *currentDep)
	}

	return lock, nil
}

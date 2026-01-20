package cas

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/greatliontech/pbr/internal/storage"
)

// Module represents a module in the CAS registry.
type Module struct {
	record   *storage.ModuleRecord
	registry *Registry
}

// ID returns the module's ID.
func (m *Module) ID() string {
	return m.record.ID
}

// OwnerID returns the module's owner ID.
func (m *Module) OwnerID() string {
	return m.record.OwnerID
}

// Owner returns the module's owner name.
func (m *Module) Owner() string {
	return m.record.Owner
}

// Name returns the module's name.
func (m *Module) Name() string {
	return m.record.Name
}

// Description returns the module's description.
func (m *Module) Description() string {
	return m.record.Description
}

// DefaultLabelName returns the module's default label name.
func (m *Module) DefaultLabelName() string {
	return m.record.DefaultLabelName
}

// CreateTime returns the module's creation time.
func (m *Module) CreateTime() time.Time {
	return m.record.CreateTime
}

// Commit retrieves a commit by label/ref name.
// If ref is empty, returns the commit for the default label.
func (m *Module) Commit(ctx context.Context, ref string) (*Commit, error) {
	slog.DebugContext(ctx, "Module.Commit", "owner", m.Owner(), "module", m.Name(), "ref", ref)

	if ref == "" {
		ref = m.record.DefaultLabelName
	}

	label, err := m.registry.metadata.GetLabel(ctx, m.record.ID, ref)
	if err != nil {
		return nil, fmt.Errorf("label %q not found: %w", ref, err)
	}

	return m.CommitByID(ctx, label.CommitID)
}

// CommitByID retrieves a commit by its ID.
func (m *Module) CommitByID(ctx context.Context, id string) (*Commit, error) {
	slog.DebugContext(ctx, "Module.CommitByID", "owner", m.Owner(), "module", m.Name(), "commitID", id)

	record, err := m.registry.metadata.GetCommit(ctx, id)
	if err != nil {
		return nil, err
	}

	// Verify commit belongs to this module
	if record.ModuleID != m.record.ID {
		return nil, storage.ErrNotFound
	}

	return &Commit{
		ID:             record.ID,
		ModuleID:       record.ModuleID,
		OwnerID:        record.OwnerID,
		ManifestDigest: record.ManifestDigest,
		CreateTime:     record.CreateTime,
		DepCommitIDs:   record.DepCommitIDs,
	}, nil
}

// FilesAndCommit retrieves files and commit info by label/ref.
func (m *Module) FilesAndCommit(ctx context.Context, ref string) ([]File, *Commit, error) {
	commit, err := m.Commit(ctx, ref)
	if err != nil {
		return nil, nil, err
	}
	files, err := m.filesForCommit(ctx, commit)
	if err != nil {
		return nil, nil, err
	}
	return files, commit, nil
}

// FilesAndCommitByCommitID retrieves files and commit info by commit ID.
func (m *Module) FilesAndCommitByCommitID(ctx context.Context, commitID string) ([]File, *Commit, error) {
	commit, err := m.CommitByID(ctx, commitID)
	if err != nil {
		return nil, nil, err
	}
	files, err := m.filesForCommit(ctx, commit)
	if err != nil {
		return nil, nil, err
	}
	return files, commit, nil
}

// filesForCommit retrieves all files for a given commit.
func (m *Module) filesForCommit(ctx context.Context, commit *Commit) ([]File, error) {
	manifest, err := m.registry.manifests.GetManifest(ctx, commit.ManifestDigest)
	if err != nil {
		return nil, fmt.Errorf("failed to get manifest: %w", err)
	}

	files := make([]File, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		rc, err := m.registry.blobs.Get(ctx, entry.Digest)
		if err != nil {
			return nil, fmt.Errorf("failed to get blob %s: %w", entry.Path, err)
		}

		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read blob %s: %w", entry.Path, err)
		}

		files = append(files, File{
			Path:    entry.Path,
			Content: string(content),
			Digest:  entry.Digest,
		})
	}

	return files, nil
}

// BufLock retrieves the parsed buf.lock for a given ref.
func (m *Module) BufLock(ctx context.Context, ref string) (*BufLock, error) {
	files, _, err := m.FilesAndCommit(ctx, ref)
	if err != nil {
		return nil, err
	}
	return m.parseBufLock(files)
}

// BufLockCommitID retrieves the parsed buf.lock for a given commit ID.
func (m *Module) BufLockCommitID(ctx context.Context, commitID string) (*BufLock, error) {
	files, _, err := m.FilesAndCommitByCommitID(ctx, commitID)
	if err != nil {
		return nil, err
	}
	return m.parseBufLock(files)
}

func (m *Module) parseBufLock(files []File) (*BufLock, error) {
	for _, f := range files {
		if f.Path == "buf.lock" {
			return ParseBufLock(f.Content)
		}
	}
	return nil, fmt.Errorf("buf.lock not found")
}

// CreateCommit creates a new commit with the given files.
// Returns the created commit or an existing commit if content is identical.
func (m *Module) CreateCommit(ctx context.Context, files []File, labels []string, sourceControlURL string, depCommitIDs []string) (*Commit, error) {
	slog.DebugContext(ctx, "Module.CreateCommit", "owner", m.Owner(), "module", m.Name(), "files", len(files), "labels", labels, "depCommitIDs", len(depCommitIDs))

	// Store blobs and build manifest
	manifest := &storage.Manifest{}

	for _, f := range files {
		digest, err := m.registry.blobs.Put(ctx, strings.NewReader(f.Content))
		if err != nil {
			return nil, fmt.Errorf("failed to store blob %s: %w", f.Path, err)
		}

		manifest.Entries = append(manifest.Entries, storage.ManifestEntry{
			Digest: digest,
			Path:   f.Path,
		})
	}

	// Store manifest
	manifestDigest, err := m.registry.manifests.PutManifest(ctx, manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to store manifest: %w", err)
	}

	// Check for existing commit with same digest (deduplication)
	existingCommit, err := m.registry.metadata.GetCommitByDigest(ctx, manifestDigest)
	if err == nil && existingCommit != nil && existingCommit.ModuleID == m.record.ID {
		slog.DebugContext(ctx, "commit already exists", "commitID", existingCommit.ID)
		// Update labels to point to existing commit
		for _, labelName := range labels {
			if err := m.updateLabel(ctx, labelName, existingCommit.ID); err != nil {
				return nil, err
			}
		}
		return &Commit{
			ID:             existingCommit.ID,
			ModuleID:       existingCommit.ModuleID,
			OwnerID:        existingCommit.OwnerID,
			ManifestDigest: existingCommit.ManifestDigest,
			CreateTime:     existingCommit.CreateTime,
			DepCommitIDs:   existingCommit.DepCommitIDs,
		}, nil
	}

	// Create new commit
	// Commit ID is UUID v7 (time-sortable) as 32 hex chars
	commitID := strings.ReplaceAll(uuid.Must(uuid.NewV7()).String(), "-", "")

	commitRecord := &storage.CommitRecord{
		ID:               commitID,
		ModuleID:         m.record.ID,
		OwnerID:          m.record.OwnerID,
		ManifestDigest:   manifestDigest,
		CreateTime:       time.Now(),
		SourceControlURL: sourceControlURL,
		DepCommitIDs:     depCommitIDs,
	}

	if err := m.registry.metadata.CreateCommit(ctx, commitRecord); err != nil {
		return nil, fmt.Errorf("failed to create commit: %w", err)
	}

	// Update labels
	for _, labelName := range labels {
		if err := m.updateLabel(ctx, labelName, commitID); err != nil {
			return nil, err
		}
	}

	return &Commit{
		ID:             commitID,
		ModuleID:       m.record.ID,
		OwnerID:        m.record.OwnerID,
		ManifestDigest: manifestDigest,
		CreateTime:     commitRecord.CreateTime,
		DepCommitIDs:   depCommitIDs,
	}, nil
}

func (m *Module) updateLabel(ctx context.Context, name, commitID string) error {
	label := &storage.LabelRecord{
		ID:       m.record.ID + "/" + name,
		ModuleID: m.record.ID,
		Name:     name,
		CommitID: commitID,
	}
	return m.registry.metadata.CreateOrUpdateLabel(ctx, label)
}

// ListCommits lists commits for this module.
func (m *Module) ListCommits(ctx context.Context, limit int, pageToken string) ([]*Commit, string, error) {
	records, nextToken, err := m.registry.metadata.ListCommits(ctx, m.record.ID, limit, pageToken)
	if err != nil {
		return nil, "", err
	}

	commits := make([]*Commit, len(records))
	for i, record := range records {
		commits[i] = &Commit{
			ID:             record.ID,
			ModuleID:       record.ModuleID,
			OwnerID:        record.OwnerID,
			ManifestDigest: record.ManifestDigest,
			CreateTime:     record.CreateTime,
			DepCommitIDs:   record.DepCommitIDs,
		}
	}

	return commits, nextToken, nil
}

// ListLabels lists all labels for this module.
func (m *Module) ListLabels(ctx context.Context) ([]*storage.LabelRecord, error) {
	return m.registry.metadata.ListLabels(ctx, m.record.ID)
}

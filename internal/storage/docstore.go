package storage

import (
	"context"
	"io"
	"time"

	"gocloud.dev/docstore"
	"gocloud.dev/gcerrors"
)

// Document types for docstore collections.
// These mirror the storage records but with docstore-compatible field tags.

// OwnerDoc is the docstore document for owners.
type OwnerDoc struct {
	ID         string    `docstore:"id"`
	Name       string    `docstore:"name"`
	CreateTime time.Time `docstore:"create_time"`
}

// ModuleDoc is the docstore document for modules.
type ModuleDoc struct {
	ID               string    `docstore:"id"`
	OwnerID          string    `docstore:"owner_id"`
	Owner            string    `docstore:"owner"`
	Name             string    `docstore:"name"`
	Description      string    `docstore:"description,omitempty"`
	DefaultLabelName string    `docstore:"default_label_name"`
	CreateTime       time.Time `docstore:"create_time"`
	UpdateTime       time.Time `docstore:"update_time"`
}

// CommitDoc is the docstore document for commits.
type CommitDoc struct {
	ID               string    `docstore:"id"`
	ModuleID         string    `docstore:"module_id"`
	OwnerID          string    `docstore:"owner_id"`
	FilesDigest      string    `docstore:"files_digest"`  // SHAKE256 digest as "shake256:hex"
	ModuleDigest     string    `docstore:"module_digest"` // module digest as "b5:hex" or "shake256:hex"
	CreateTime       time.Time `docstore:"create_time"`
	CreatedByUserID  string    `docstore:"created_by_user_id,omitempty"`
	SourceControlURL string    `docstore:"source_control_url,omitempty"`
	DepCommitIDs     []string  `docstore:"dep_commit_ids,omitempty"`
}

// LabelDoc is the docstore document for labels.
type LabelDoc struct {
	ID       string `docstore:"id"` // derived from moduleID + "/" + name
	ModuleID string `docstore:"module_id"`
	Name     string `docstore:"name"`
	CommitID string `docstore:"commit_id"`
}

// MetadataStoreImpl implements MetadataStore using gocloud.dev/docstore.
type MetadataStoreImpl struct {
	owners  *docstore.Collection
	modules *docstore.Collection
	commits *docstore.Collection
	labels  *docstore.Collection
}

// NewMetadataStore creates a new gocloud.dev/docstore-backed metadata store.
func NewMetadataStore(owners, modules, commits, labels *docstore.Collection) *MetadataStoreImpl {
	return &MetadataStoreImpl{
		owners:  owners,
		modules: modules,
		commits: commits,
		labels:  labels,
	}
}

// Close closes all docstore collections.
func (s *MetadataStoreImpl) Close() error {
	var errs []error
	if err := s.owners.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := s.modules.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := s.commits.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := s.labels.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// ----- Owner operations -----

func (s *MetadataStoreImpl) GetOwner(ctx context.Context, id string) (*OwnerRecord, error) {
	doc := &OwnerDoc{ID: id}
	if err := s.owners.Get(ctx, doc); err != nil {
		if gcerrors.Code(err) == gcerrors.NotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &OwnerRecord{
		ID:         doc.ID,
		Name:       doc.Name,
		CreateTime: doc.CreateTime,
	}, nil
}

func (s *MetadataStoreImpl) GetOwnerByName(ctx context.Context, name string) (*OwnerRecord, error) {
	iter := s.owners.Query().Where("name", "=", name).Get(ctx)
	defer iter.Stop()

	doc := &OwnerDoc{}
	if err := iter.Next(ctx, doc); err != nil {
		if err == io.EOF {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &OwnerRecord{
		ID:         doc.ID,
		Name:       doc.Name,
		CreateTime: doc.CreateTime,
	}, nil
}

func (s *MetadataStoreImpl) CreateOwner(ctx context.Context, owner *OwnerRecord) error {
	doc := &OwnerDoc{
		ID:         owner.ID,
		Name:       owner.Name,
		CreateTime: owner.CreateTime,
	}
	if err := s.owners.Create(ctx, doc); err != nil {
		if gcerrors.Code(err) == gcerrors.AlreadyExists {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (s *MetadataStoreImpl) ListOwners(ctx context.Context) ([]*OwnerRecord, error) {
	iter := s.owners.Query().Get(ctx)
	defer iter.Stop()

	var owners []*OwnerRecord
	for {
		doc := &OwnerDoc{}
		if err := iter.Next(ctx, doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		owners = append(owners, &OwnerRecord{
			ID:         doc.ID,
			Name:       doc.Name,
			CreateTime: doc.CreateTime,
		})
	}
	return owners, nil
}

// ----- Module operations -----

func (s *MetadataStoreImpl) GetModule(ctx context.Context, id string) (*ModuleRecord, error) {
	doc := &ModuleDoc{ID: id}
	if err := s.modules.Get(ctx, doc); err != nil {
		if gcerrors.Code(err) == gcerrors.NotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return moduleDocToRecord(doc), nil
}

func (s *MetadataStoreImpl) GetModuleByName(ctx context.Context, owner, name string) (*ModuleRecord, error) {
	iter := s.modules.Query().Where("owner", "=", owner).Where("name", "=", name).Get(ctx)
	defer iter.Stop()

	doc := &ModuleDoc{}
	if err := iter.Next(ctx, doc); err != nil {
		if err == io.EOF {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return moduleDocToRecord(doc), nil
}

func (s *MetadataStoreImpl) ListModules(ctx context.Context, ownerID string) ([]*ModuleRecord, error) {
	iter := s.modules.Query().Where("owner_id", "=", ownerID).Get(ctx)
	defer iter.Stop()

	var modules []*ModuleRecord
	for {
		doc := &ModuleDoc{}
		if err := iter.Next(ctx, doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		modules = append(modules, moduleDocToRecord(doc))
	}
	return modules, nil
}

func (s *MetadataStoreImpl) CreateModule(ctx context.Context, module *ModuleRecord) error {
	doc := moduleRecordToDoc(module)
	if err := s.modules.Create(ctx, doc); err != nil {
		if gcerrors.Code(err) == gcerrors.AlreadyExists {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (s *MetadataStoreImpl) UpdateModule(ctx context.Context, module *ModuleRecord) error {
	doc := moduleRecordToDoc(module)
	return s.modules.Replace(ctx, doc)
}

func (s *MetadataStoreImpl) DeleteModule(ctx context.Context, id string) error {
	doc := &ModuleDoc{ID: id}
	err := s.modules.Delete(ctx, doc)
	if err != nil && gcerrors.Code(err) == gcerrors.NotFound {
		return nil
	}
	return err
}

func moduleDocToRecord(doc *ModuleDoc) *ModuleRecord {
	return &ModuleRecord{
		ID:               doc.ID,
		OwnerID:          doc.OwnerID,
		Owner:            doc.Owner,
		Name:             doc.Name,
		Description:      doc.Description,
		DefaultLabelName: doc.DefaultLabelName,
		CreateTime:       doc.CreateTime,
		UpdateTime:       doc.UpdateTime,
	}
}

func moduleRecordToDoc(m *ModuleRecord) *ModuleDoc {
	return &ModuleDoc{
		ID:               m.ID,
		OwnerID:          m.OwnerID,
		Owner:            m.Owner,
		Name:             m.Name,
		Description:      m.Description,
		DefaultLabelName: m.DefaultLabelName,
		CreateTime:       m.CreateTime,
		UpdateTime:       m.UpdateTime,
	}
}

// ----- Commit operations -----

func (s *MetadataStoreImpl) GetCommit(ctx context.Context, id string) (*CommitRecord, error) {
	doc := &CommitDoc{ID: id}
	if err := s.commits.Get(ctx, doc); err != nil {
		if gcerrors.Code(err) == gcerrors.NotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return commitDocToRecord(doc)
}

func (s *MetadataStoreImpl) GetCommitByFilesDigest(ctx context.Context, digest Digest) (*CommitRecord, error) {
	digestStr := digest.String()
	iter := s.commits.Query().Where("files_digest", "=", digestStr).Get(ctx)
	defer iter.Stop()

	doc := &CommitDoc{}
	if err := iter.Next(ctx, doc); err != nil {
		if err == io.EOF {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return commitDocToRecord(doc)
}

func (s *MetadataStoreImpl) ListCommits(ctx context.Context, moduleID string, limit int, pageToken string) ([]*CommitRecord, string, error) {
	if limit <= 0 {
		limit = 100
	}

	q := s.commits.Query().Where("module_id", "=", moduleID)

	iter := q.Get(ctx)
	defer iter.Stop()

	var allCommits []*CommitRecord

	// Collect all commits first (we need to sort them)
	for {
		doc := &CommitDoc{}
		if err := iter.Next(ctx, doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, "", err
		}
		commit, err := commitDocToRecord(doc)
		if err != nil {
			continue
		}
		allCommits = append(allCommits, commit)
	}

	// Sort by ID descending (UUID v7 is time-sortable)
	sortCommitsByIDDesc(allCommits)

	// Handle pagination
	startIdx := 0
	if pageToken != "" {
		for i, c := range allCommits {
			if c.ID == pageToken {
				startIdx = i + 1
				break
			}
		}
	}

	var commits []*CommitRecord
	var nextToken string
	for i := startIdx; i < len(allCommits) && len(commits) < limit; i++ {
		commits = append(commits, allCommits[i])
		if len(commits) == limit && i+1 < len(allCommits) {
			nextToken = allCommits[i].ID
		}
	}

	return commits, nextToken, nil
}

func (s *MetadataStoreImpl) CreateCommit(ctx context.Context, commit *CommitRecord) error {
	doc := commitRecordToDoc(commit)
	if err := s.commits.Create(ctx, doc); err != nil {
		// Already exists is ok for idempotency
		if gcerrors.Code(err) == gcerrors.AlreadyExists {
			return nil
		}
		return err
	}
	return nil
}

func commitDocToRecord(doc *CommitDoc) (*CommitRecord, error) {
	filesDigest, err := ParseDigest(doc.FilesDigest)
	if err != nil {
		return nil, err
	}
	var moduleDigest ModuleDigest
	if doc.ModuleDigest != "" {
		moduleDigest, err = ParseModuleDigest(doc.ModuleDigest)
		if err != nil {
			return nil, err
		}
	}
	return &CommitRecord{
		ID:               doc.ID,
		ModuleID:         doc.ModuleID,
		OwnerID:          doc.OwnerID,
		FilesDigest:      filesDigest,
		ModuleDigest:     moduleDigest,
		CreateTime:       doc.CreateTime,
		CreatedByUserID:  doc.CreatedByUserID,
		SourceControlURL: doc.SourceControlURL,
		DepCommitIDs:     doc.DepCommitIDs,
	}, nil
}

func commitRecordToDoc(c *CommitRecord) *CommitDoc {
	return &CommitDoc{
		ID:               c.ID,
		ModuleID:         c.ModuleID,
		OwnerID:          c.OwnerID,
		FilesDigest:      c.FilesDigest.String(),
		ModuleDigest:     c.ModuleDigest.String(),
		CreateTime:       c.CreateTime,
		CreatedByUserID:  c.CreatedByUserID,
		SourceControlURL: c.SourceControlURL,
		DepCommitIDs:     c.DepCommitIDs,
	}
}

func sortCommitsByIDDesc(commits []*CommitRecord) {
	// Simple sort for now (lists are typically small)
	for i := 0; i < len(commits); i++ {
		for j := i + 1; j < len(commits); j++ {
			if commits[i].ID < commits[j].ID {
				commits[i], commits[j] = commits[j], commits[i]
			}
		}
	}
}

// ----- Label operations -----

func labelID(moduleID, name string) string {
	return moduleID + "/" + name
}

func (s *MetadataStoreImpl) GetLabel(ctx context.Context, moduleID, name string) (*LabelRecord, error) {
	doc := &LabelDoc{ID: labelID(moduleID, name)}
	if err := s.labels.Get(ctx, doc); err != nil {
		if gcerrors.Code(err) == gcerrors.NotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &LabelRecord{
		ID:       doc.ID,
		ModuleID: doc.ModuleID,
		Name:     doc.Name,
		CommitID: doc.CommitID,
	}, nil
}

func (s *MetadataStoreImpl) ListLabels(ctx context.Context, moduleID string) ([]*LabelRecord, error) {
	iter := s.labels.Query().Where("module_id", "=", moduleID).Get(ctx)
	defer iter.Stop()

	var labels []*LabelRecord
	for {
		doc := &LabelDoc{}
		if err := iter.Next(ctx, doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		labels = append(labels, &LabelRecord{
			ID:       doc.ID,
			ModuleID: doc.ModuleID,
			Name:     doc.Name,
			CommitID: doc.CommitID,
		})
	}
	return labels, nil
}

func (s *MetadataStoreImpl) CreateOrUpdateLabel(ctx context.Context, label *LabelRecord) error {
	doc := &LabelDoc{
		ID:       labelID(label.ModuleID, label.Name),
		ModuleID: label.ModuleID,
		Name:     label.Name,
		CommitID: label.CommitID,
	}
	return s.labels.Put(ctx, doc)
}

func (s *MetadataStoreImpl) DeleteLabel(ctx context.Context, moduleID, name string) error {
	doc := &LabelDoc{ID: labelID(moduleID, name)}
	err := s.labels.Delete(ctx, doc)
	if err != nil && gcerrors.Code(err) == gcerrors.NotFound {
		return nil
	}
	return err
}

package filesystem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/greatliontech/pbr/internal/storage"
)

// MetadataStore implements storage.MetadataStore using the filesystem.
// Structure:
//
//	<basePath>/owners/<id>.json
//	<basePath>/modules/<id>.json
//	<basePath>/commits/<id>.json
//	<basePath>/labels/<module-id>/<name>.json
//	<basePath>/index/owner-modules/<owner-id>.json    -> []moduleID
//	<basePath>/index/module-commits/<module-id>.json  -> []commitID
type MetadataStore struct {
	basePath string
	mu       sync.RWMutex
}

// NewMetadataStore creates a new filesystem-backed metadata store.
func NewMetadataStore(basePath string) *MetadataStore {
	return &MetadataStore{basePath: basePath}
}

// Helper to read JSON file into a struct.
func readJSON(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// Helper to write JSON file atomically.
func writeJSON(path string, v interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(path), "*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return err
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return os.Rename(tmpPath, path)
}

// ----- Owner operations -----

func (s *MetadataStore) ownerPath(id string) string {
	return filepath.Join(s.basePath, "owners", id+".json")
}

func (s *MetadataStore) GetOwner(ctx context.Context, id string) (*storage.OwnerRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var owner storage.OwnerRecord
	if err := readJSON(s.ownerPath(id), &owner); err != nil {
		if os.IsNotExist(err) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	return &owner, nil
}

func (s *MetadataStore) GetOwnerByName(ctx context.Context, name string) (*storage.OwnerRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Scan owners directory
	ownersDir := filepath.Join(s.basePath, "owners")
	entries, err := os.ReadDir(ownersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		var owner storage.OwnerRecord
		if err := readJSON(filepath.Join(ownersDir, entry.Name()), &owner); err != nil {
			continue
		}
		if owner.Name == name {
			return &owner, nil
		}
	}
	return nil, storage.ErrNotFound
}

func (s *MetadataStore) CreateOwner(ctx context.Context, owner *storage.OwnerRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.ownerPath(owner.ID)
	if _, err := os.Stat(path); err == nil {
		return storage.ErrAlreadyExists
	}

	return writeJSON(path, owner)
}

func (s *MetadataStore) ListOwners(ctx context.Context) ([]*storage.OwnerRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ownersDir := filepath.Join(s.basePath, "owners")
	entries, err := os.ReadDir(ownersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var owners []*storage.OwnerRecord
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		var owner storage.OwnerRecord
		if err := readJSON(filepath.Join(ownersDir, entry.Name()), &owner); err != nil {
			continue
		}
		owners = append(owners, &owner)
	}
	return owners, nil
}

// ----- Module operations -----

func (s *MetadataStore) modulePath(id string) string {
	return filepath.Join(s.basePath, "modules", id+".json")
}

func (s *MetadataStore) ownerModulesIndexPath(ownerID string) string {
	return filepath.Join(s.basePath, "index", "owner-modules", ownerID+".json")
}

func (s *MetadataStore) GetModule(ctx context.Context, id string) (*storage.ModuleRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var module storage.ModuleRecord
	if err := readJSON(s.modulePath(id), &module); err != nil {
		if os.IsNotExist(err) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	return &module, nil
}

func (s *MetadataStore) GetModuleByName(ctx context.Context, owner, name string) (*storage.ModuleRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Scan modules directory
	modulesDir := filepath.Join(s.basePath, "modules")
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		var module storage.ModuleRecord
		if err := readJSON(filepath.Join(modulesDir, entry.Name()), &module); err != nil {
			continue
		}
		if module.Owner == owner && module.Name == name {
			return &module, nil
		}
	}
	return nil, storage.ErrNotFound
}

func (s *MetadataStore) ListModules(ctx context.Context, ownerID string) ([]*storage.ModuleRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Read from index
	var moduleIDs []string
	indexPath := s.ownerModulesIndexPath(ownerID)
	if err := readJSON(indexPath, &moduleIDs); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var modules []*storage.ModuleRecord
	for _, id := range moduleIDs {
		var module storage.ModuleRecord
		if err := readJSON(s.modulePath(id), &module); err != nil {
			continue
		}
		modules = append(modules, &module)
	}
	return modules, nil
}

func (s *MetadataStore) CreateModule(ctx context.Context, module *storage.ModuleRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.modulePath(module.ID)
	if _, err := os.Stat(path); err == nil {
		return storage.ErrAlreadyExists
	}

	if err := writeJSON(path, module); err != nil {
		return err
	}

	// Update index
	return s.addToOwnerModulesIndex(module.OwnerID, module.ID)
}

func (s *MetadataStore) addToOwnerModulesIndex(ownerID, moduleID string) error {
	indexPath := s.ownerModulesIndexPath(ownerID)

	var moduleIDs []string
	if err := readJSON(indexPath, &moduleIDs); err != nil && !os.IsNotExist(err) {
		return err
	}

	// Check if already in index
	for _, id := range moduleIDs {
		if id == moduleID {
			return nil
		}
	}

	moduleIDs = append(moduleIDs, moduleID)
	return writeJSON(indexPath, moduleIDs)
}

func (s *MetadataStore) UpdateModule(ctx context.Context, module *storage.ModuleRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.modulePath(module.ID)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return storage.ErrNotFound
	}

	return writeJSON(path, module)
}

func (s *MetadataStore) DeleteModule(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Read module first to get ownerID
	var module storage.ModuleRecord
	if err := readJSON(s.modulePath(id), &module); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	// Remove from index
	if err := s.removeFromOwnerModulesIndex(module.OwnerID, id); err != nil {
		return err
	}

	// Delete module file
	return os.Remove(s.modulePath(id))
}

func (s *MetadataStore) removeFromOwnerModulesIndex(ownerID, moduleID string) error {
	indexPath := s.ownerModulesIndexPath(ownerID)

	var moduleIDs []string
	if err := readJSON(indexPath, &moduleIDs); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	// Remove moduleID
	var newIDs []string
	for _, id := range moduleIDs {
		if id != moduleID {
			newIDs = append(newIDs, id)
		}
	}

	if len(newIDs) == 0 {
		return os.Remove(indexPath)
	}

	return writeJSON(indexPath, newIDs)
}

// ----- Commit operations -----

func (s *MetadataStore) commitPath(id string) string {
	return filepath.Join(s.basePath, "commits", id+".json")
}

func (s *MetadataStore) moduleCommitsIndexPath(moduleID string) string {
	return filepath.Join(s.basePath, "index", "module-commits", moduleID+".json")
}

// commitJSON is the JSON-serializable form of CommitRecord.
type commitJSON struct {
	ID               string   `json:"id"`
	ModuleID         string   `json:"module_id"`
	OwnerID          string   `json:"owner_id"`
	ManifestDigest   string   `json:"manifest_digest"`
	CreateTime       string   `json:"create_time"`
	CreatedByUserID  string   `json:"created_by_user_id,omitempty"`
	SourceControlURL string   `json:"source_control_url,omitempty"`
	DepCommitIDs     []string `json:"dep_commit_ids,omitempty"`
}

func commitToJSON(c *storage.CommitRecord) *commitJSON {
	return &commitJSON{
		ID:               c.ID,
		ModuleID:         c.ModuleID,
		OwnerID:          c.OwnerID,
		ManifestDigest:   c.ManifestDigest.String(),
		CreateTime:       c.CreateTime.Format(time.RFC3339Nano),
		CreatedByUserID:  c.CreatedByUserID,
		SourceControlURL: c.SourceControlURL,
		DepCommitIDs:     c.DepCommitIDs,
	}
}

func commitFromJSON(cj *commitJSON) (*storage.CommitRecord, error) {
	digest, err := storage.ParseDigest(cj.ManifestDigest)
	if err != nil {
		return nil, err
	}
	var createTime time.Time
	if cj.CreateTime != "" {
		// Try RFC3339Nano first (new format), fall back to RFC3339 (old format)
		createTime, err = time.Parse(time.RFC3339Nano, cj.CreateTime)
		if err != nil {
			createTime, err = time.Parse(time.RFC3339, cj.CreateTime)
			if err != nil {
				return nil, err
			}
		}
	}
	return &storage.CommitRecord{
		ID:               cj.ID,
		ModuleID:         cj.ModuleID,
		OwnerID:          cj.OwnerID,
		ManifestDigest:   digest,
		CreateTime:       createTime,
		CreatedByUserID:  cj.CreatedByUserID,
		SourceControlURL: cj.SourceControlURL,
		DepCommitIDs:     cj.DepCommitIDs,
	}, nil
}

func (s *MetadataStore) GetCommit(ctx context.Context, id string) (*storage.CommitRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var cj commitJSON
	if err := readJSON(s.commitPath(id), &cj); err != nil {
		if os.IsNotExist(err) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	return commitFromJSON(&cj)
}

func (s *MetadataStore) GetCommitByDigest(ctx context.Context, digest storage.Digest) (*storage.CommitRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Scan commits directory
	commitsDir := filepath.Join(s.basePath, "commits")
	entries, err := os.ReadDir(commitsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}

	digestStr := digest.String()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		var cj commitJSON
		if err := readJSON(filepath.Join(commitsDir, entry.Name()), &cj); err != nil {
			continue
		}
		if cj.ManifestDigest == digestStr {
			return commitFromJSON(&cj)
		}
	}
	return nil, storage.ErrNotFound
}

func (s *MetadataStore) ListCommits(ctx context.Context, moduleID string, limit int, pageToken string) ([]*storage.CommitRecord, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Read from index
	var commitIDs []string
	indexPath := s.moduleCommitsIndexPath(moduleID)
	if err := readJSON(indexPath, &commitIDs); err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", err
	}

	// Sort by most recent first (reverse order since we append)
	sort.Sort(sort.Reverse(sort.StringSlice(commitIDs)))

	// Handle pagination
	startIdx := 0
	if pageToken != "" {
		for i, id := range commitIDs {
			if id == pageToken {
				startIdx = i + 1
				break
			}
		}
	}

	if limit <= 0 {
		limit = 100
	}

	var commits []*storage.CommitRecord
	var nextToken string

	for i := startIdx; i < len(commitIDs) && len(commits) < limit; i++ {
		var cj commitJSON
		if err := readJSON(s.commitPath(commitIDs[i]), &cj); err != nil {
			continue
		}
		commit, err := commitFromJSON(&cj)
		if err != nil {
			continue
		}
		commits = append(commits, commit)
		if len(commits) == limit && i+1 < len(commitIDs) {
			nextToken = commitIDs[i]
		}
	}

	return commits, nextToken, nil
}

func (s *MetadataStore) CreateCommit(ctx context.Context, commit *storage.CommitRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.commitPath(commit.ID)
	if _, err := os.Stat(path); err == nil {
		// Already exists - this is ok for idempotency
		return nil
	}

	cj := commitToJSON(commit)
	if err := writeJSON(path, cj); err != nil {
		return err
	}

	// Update index
	return s.addToModuleCommitsIndex(commit.ModuleID, commit.ID)
}

func (s *MetadataStore) addToModuleCommitsIndex(moduleID, commitID string) error {
	indexPath := s.moduleCommitsIndexPath(moduleID)

	var commitIDs []string
	if err := readJSON(indexPath, &commitIDs); err != nil && !os.IsNotExist(err) {
		return err
	}

	// Check if already in index
	for _, id := range commitIDs {
		if id == commitID {
			return nil
		}
	}

	commitIDs = append(commitIDs, commitID)
	return writeJSON(indexPath, commitIDs)
}

// ----- Label operations -----

func (s *MetadataStore) labelPath(moduleID, name string) string {
	return filepath.Join(s.basePath, "labels", moduleID, name+".json")
}

func (s *MetadataStore) GetLabel(ctx context.Context, moduleID, name string) (*storage.LabelRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var label storage.LabelRecord
	if err := readJSON(s.labelPath(moduleID, name), &label); err != nil {
		if os.IsNotExist(err) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	return &label, nil
}

func (s *MetadataStore) ListLabels(ctx context.Context, moduleID string) ([]*storage.LabelRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	labelsDir := filepath.Join(s.basePath, "labels", moduleID)
	entries, err := os.ReadDir(labelsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var labels []*storage.LabelRecord
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		var label storage.LabelRecord
		if err := readJSON(filepath.Join(labelsDir, entry.Name()), &label); err != nil {
			continue
		}
		labels = append(labels, &label)
	}
	return labels, nil
}

func (s *MetadataStore) CreateOrUpdateLabel(ctx context.Context, label *storage.LabelRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return writeJSON(s.labelPath(label.ModuleID, label.Name), label)
}

func (s *MetadataStore) DeleteLabel(ctx context.Context, moduleID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.labelPath(moduleID, name)
	err := os.Remove(path)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}

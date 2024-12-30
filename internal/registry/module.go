package registry

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/gobwas/glob"
	"github.com/greatliontech/pbr/internal/repository"
	"github.com/greatliontech/pbr/internal/store"
	"github.com/greatliontech/pbr/internal/store/mem"
	"golang.org/x/crypto/sha3"
)

type Module struct {
	module        *store.Module
	repo          *repository.Repository
	shake256Cache map[string]string
	filters       []glob.Glob

	commitsCache mem.SyncMap[string, *Commit]
}

type File struct {
	Name     string
	SHA      string
	Content  string
	SHAKE256 string
}

type Manifest struct {
	Commit   string
	Content  string
	SHAKE256 string
}

type Commit struct {
	ModuleId string
	OwnerId  string
	CommitId string
	Disgest  string
}

var filters = []glob.Glob{
	glob.MustCompile("**.proto"),
	glob.MustCompile("buf.yaml"),
	glob.MustCompile("buf.lock"),
}

func NewModule(module *store.Module, repo *repository.Repository) *Module {
	return &Module{
		module:        module,
		repo:          repo,
		filters:       filters,
		shake256Cache: make(map[string]string),
	}
}

func (m *Module) Commit(ref string) (*Commit, error) {
	if ref == "" {
		ref = "HEAD"
	}
	// find commit from cache first
	if c, ok := m.commitsCache.Load(ref); ok {
		return c, nil
	}
	_, mani, err := m.FilesAndManifest(ref)
	if err != nil {
		return nil, err
	}
	c := &Commit{
		ModuleId: m.module.ID,
		OwnerId:  m.module.OwnerID,
		CommitId: mani.Commit[:32],
		Disgest:  mani.SHAKE256,
	}
	m.commitsCache.Store(ref, c)
	return c, nil
}

func (m *Module) FilesAndManifest(ref string) ([]File, *Manifest, error) {
	commit, repoFiles, err := m.repo.Files(ref, m.module.Root, m.filters...)
	if err != nil {
		return nil, nil, err
	}

	return m.filesAndManifest(commit, repoFiles)
}

func (m *Module) FilesAndManifestCommit(cmmt string) ([]File, *Manifest, error) {
	commit, repoFiles, err := m.repo.FilesCommit(cmmt, m.module.Root, m.filters...)
	if err != nil {
		return nil, nil, err
	}

	return m.filesAndManifest(commit, repoFiles)
}

var ErrBufLockNotFound = fmt.Errorf("buf.lock not found")

func (m *Module) BufLock(ref string) (*BufLock, error) {
	slog.Debug("module buf lock", "ref", ref)
	_, repoFiles, err := m.repo.Files(ref, m.module.Root, m.filters...)
	if err != nil {
		return nil, err
	}
	for _, f := range repoFiles {
		if f.Name == "buf.lock" {
			return BufLockFromBytes([]byte(f.Content))
		}
	}
	slog.Debug("buf.lock not found", "ref", ref)
	return nil, ErrBufLockNotFound
}

func (m *Module) BufLockCommit(cmmt string) (*BufLock, error) {
	slog.Debug("module buf lock commit", "commit", cmmt)
	_, repoFiles, err := m.repo.FilesCommit(cmmt, m.module.Root, m.filters...)
	if err != nil {
		return nil, err
	}
	for _, f := range repoFiles {
		if f.Name == "buf.lock" {
			return BufLockFromBytes([]byte(f.Content))
		}
	}
	slog.Debug("buf.lock not found", "commit", cmmt)
	return nil, ErrBufLockNotFound
}

func (m *Module) HasCommitId(cid string) (bool, string, error) {
	c, err := m.repo.CommitFromShort(cid)
	if err == nil {
		return true, c.Hash.String(), nil
	}
	if err == repository.ErrCommitNotFound {
		return false, "", nil
	}
	return false, "", err
}

func (m *Module) filesAndManifest(commit *object.Commit, repoFiles []repository.File) ([]File, *Manifest, error) {
	var files []File
	var manifestContentBuilder strings.Builder

	for _, file := range repoFiles {
		shake256Hash, ok := m.shake256Cache[file.SHA]
		if !ok {
			// calculate SHAKE256 hash
			h := sha3.NewShake256()
			_, err := h.Write([]byte(file.Content))
			if err != nil {
				return nil, nil, err
			}
			var shake256Sum [64]byte
			h.Read(shake256Sum[:])
			shake256Hash = fmt.Sprintf("%x", shake256Sum)
			m.shake256Cache[file.SHA] = shake256Hash
		}

		files = append(files, File{
			Name:     file.Name,
			SHA:      file.SHA,
			Content:  file.Content,
			SHAKE256: shake256Hash,
		})

		// add file info to the manifest content
		manifestContentBuilder.WriteString(fmt.Sprintf("shake256:%s  %s\n", files[len(files)-1].SHAKE256, file.Name))
	}

	// generate manifest
	manifestContent := manifestContentBuilder.String()
	manifest := &Manifest{
		Commit:  commit.Hash.String(),
		Content: manifestContent,
	}

	// calculate SHAKE256 hash for the manifest content
	h := sha3.NewShake256()
	_, err := h.Write([]byte(manifestContent))
	if err != nil {
		return nil, nil, err
	}
	var shake256Sum [64]byte
	h.Read(shake256Sum[:])
	manifest.SHAKE256 = fmt.Sprintf("%x", shake256Sum)

	return files, manifest, nil
}

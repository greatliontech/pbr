package registry

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/gobwas/glob"
	"github.com/greatliontech/pbr/internal/repository"
	"github.com/greatliontech/pbr/internal/store"
	"github.com/greatliontech/pbr/internal/store/mem"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/sha3"
)

type Module struct {
	*store.Module
	repo          *repository.Repository
	shake256Cache map[string]string
	filters       []glob.Glob

	commitsByRefCache mem.SyncMap[string, *Commit]
	commitsByCidCache mem.SyncMap[string, *Commit]

	reg *Registry
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

func newModule(reg *Registry, module *store.Module, repo *repository.Repository) *Module {
	return &Module{
		Module:        module,
		repo:          repo,
		filters:       filters,
		shake256Cache: make(map[string]string),
		reg:           reg,
	}
}

func (m *Module) Commit(ref string) (*Commit, error) {
	if ref == "" {
		ref = "HEAD"
	}
	// find commit from cache first
	if c, ok := m.commitsByRefCache.Load(ref); ok {
		return c, nil
	}
	_, mani, err := m.FilesAndManifest(ref)
	if err != nil {
		return nil, err
	}
	c := &Commit{
		ModuleId: m.ID,
		OwnerId:  m.OwnerID,
		CommitId: mani.Commit[:32],
		Disgest:  mani.SHAKE256,
	}
	m.commitsByRefCache.Store(ref, c)
	m.commitsByCidCache.Store(c.CommitId, c)
	return c, nil
}

func (m *Module) CommitById(cid string) (*Commit, error) {
	// find commit from cache first
	if c, ok := m.commitsByCidCache.Load(cid); ok {
		return c, nil
	}
	_, mani, err := m.FilesAndManifestCommit(cid)
	if err != nil {
		return nil, err
	}
	c := &Commit{
		ModuleId: m.ID,
		OwnerId:  m.OwnerID,
		CommitId: mani.Commit[:32],
		Disgest:  mani.SHAKE256,
	}
	m.commitsByRefCache.Store(mani.Commit, c)
	m.commitsByCidCache.Store(c.CommitId, c)
	return c, nil
}

func (m *Module) FilesAndManifest(ref string) ([]File, *Manifest, error) {
	commit, repoFiles, err := m.repo.Files(ref, m.Root, m.filters...)
	if err != nil {
		return nil, nil, err
	}

	return m.filesAndManifest(commit, repoFiles)
}

func (m *Module) FilesAndManifestCommit(cmmt string) ([]File, *Manifest, error) {
	commit, repoFiles, err := m.repo.FilesCommit(cmmt, m.Root, m.filters...)
	if err != nil {
		return nil, nil, err
	}

	return m.filesAndManifest(commit, repoFiles)
}

var ErrBufLockNotFound = fmt.Errorf("buf.lock not found")

func (m *Module) BufLock(ctx context.Context, ref string) (*BufLock, error) {
	ctx, span := tracer.Start(ctx, "BufLock", trace.WithAttributes(
		attribute.String("ref", ref),
	))
	defer span.End()

	cmt, repoFiles, err := m.repo.Files(ref, m.Root, m.filters...)
	if err != nil {
		return nil, err
	}
	return m.bufLock(ctx, cmt.Hash.String()[:32], repoFiles)
}

func (m *Module) BufLockCommitId(ctx context.Context, cmmt string) (*BufLock, error) {
	ctx, span := tracer.Start(ctx, "BufLockCommitId", trace.WithAttributes(
		attribute.String("commitId", cmmt),
	))
	defer span.End()

	_, repoFiles, err := m.repo.FilesCommit(cmmt, m.Root, m.filters...)
	if err != nil {
		return nil, err
	}
	return m.bufLock(ctx, cmmt, repoFiles)
}

func (m *Module) bufLock(ctx context.Context, commitId string, repoFiles []repository.File) (*BufLock, error) {
	for _, f := range repoFiles {
		if f.Name == "buf.lock" {
			bl, err := BufLockFromBytes([]byte(f.Content))
			if err != nil {
				return nil, err
			}
			for _, d := range bl.Deps {
				if d.Remote == m.reg.hostName {
					if err := m.reg.addToCache(ctx, commitId, m.Owner, d.Repository); err != nil {
						return nil, err
					}
				}
			}
		}
	}
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

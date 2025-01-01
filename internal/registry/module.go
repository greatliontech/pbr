package registry

import (
	"context"
	"fmt"

	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/gobwas/glob"
	"github.com/greatliontech/pbr/internal/config"
	"github.com/greatliontech/pbr/internal/repository"
	"github.com/greatliontech/pbr/internal/util"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/sha3"
)

type Module struct {
	config.Module
	Owner         string
	OwnerID       string
	Name          string
	ID            string
	repo          *repository.Repository
	shake256Cache map[string]string
	filters       []glob.Glob

	commitsByRefCache util.SyncMap[string, *Commit]
	commitsByCidCache util.SyncMap[string, *Commit]

	reg *Registry
}

type File struct {
	Name     string
	SHA      string
	Content  string
	SHAKE256 string
}

type Commit struct {
	ModuleId string
	OwnerId  string
	CommitId string
	Digest   string
}

var filters = []glob.Glob{
	glob.MustCompile("**.proto"),
	glob.MustCompile("buf.yaml"),
	glob.MustCompile("buf.lock"),
}

func newModule(reg *Registry, owner, name string, module config.Module, repo *repository.Repository) *Module {
	ownerId := util.OwnerID(owner)
	modId := util.ModuleID(ownerId, name)
	return &Module{
		ID:            modId,
		OwnerID:       ownerId,
		Owner:         owner,
		Name:          name,
		Module:        module,
		repo:          repo,
		filters:       filters,
		shake256Cache: make(map[string]string),
		reg:           reg,
	}
}

func (m *Module) Commit(ctx context.Context, ref string) (*Commit, error) {
	ctx, span := tracer.Start(ctx, "Module.Commit", trace.WithAttributes(
		attribute.String("ref", ref),
	))
	defer span.End()

	if ref == "" {
		ref = "HEAD"
	}
	// find commit from cache first
	if c, ok := m.commitsByRefCache.Load(ref); ok {
		return c, nil
	}
	_, c, err := m.FilesAndCommit(ctx, ref)
	if err != nil {
		return nil, err
	}
	m.commitsByRefCache.Store(ref, c)
	m.commitsByCidCache.Store(c.CommitId, c)
	return c, nil
}

func (m *Module) CommitById(ctx context.Context, cid string) (*Commit, error) {
	ctx, span := tracer.Start(ctx, "Module.CommitById", trace.WithAttributes(
		attribute.String("commitId", cid),
	))
	defer span.End()

	// find commit from cache first
	if c, ok := m.commitsByCidCache.Load(cid); ok {
		return c, nil
	}
	_, c, err := m.FilesAndCommitByCommitId(ctx, cid)
	if err != nil {
		return nil, err
	}
	m.commitsByCidCache.Store(c.CommitId, c)
	return c, nil
}

func (m *Module) FilesAndCommit(ctx context.Context, ref string) ([]File, *Commit, error) {
	ctx, span := tracer.Start(ctx, "Module.FilesAndCommit", trace.WithAttributes(
		attribute.String("ref", ref),
	))
	defer span.End()

	commit, repoFiles, err := m.repo.Files(ctx, ref, m.Path, m.filters...)
	if err != nil {
		return nil, nil, err
	}

	return m.filesAndCommit(commit, repoFiles)
}

func (m *Module) FilesAndCommitByCommitId(ctx context.Context, cmmt string) ([]File, *Commit, error) {
	ctx, span := tracer.Start(ctx, "Module.FilesAndCommitByCommitId", trace.WithAttributes(
		attribute.String("commitId", cmmt),
	))
	defer span.End()

	commit, repoFiles, err := m.repo.FilesCommit(ctx, cmmt, m.Path, m.filters...)
	if err != nil {
		return nil, nil, err
	}

	return m.filesAndCommit(commit, repoFiles)
}

var ErrBufLockNotFound = fmt.Errorf("buf.lock not found")

func (m *Module) BufLock(ctx context.Context, ref string) (*BufLock, error) {
	ctx, span := tracer.Start(ctx, "BufLock", trace.WithAttributes(
		attribute.String("ref", ref),
	))
	defer span.End()

	cmt, repoFiles, err := m.repo.Files(ctx, ref, m.Path, m.filters...)
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

	_, repoFiles, err := m.repo.FilesCommit(ctx, cmmt, m.Path, m.filters...)
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
					if err := m.reg.addToCache(ctx, commitId, d.Owner, d.Repository); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	return nil, ErrBufLockNotFound
}

func (m *Module) HasCommitId(ctx context.Context, cid string) (bool, string, error) {
	ctx, span := tracer.Start(ctx, "Module.HasCommitId", trace.WithAttributes(
		attribute.String("commitId", cid),
	))
	defer span.End()

	c, err := m.repo.CommitFromShort(ctx, cid)
	if err == nil {
		return true, c.Hash.String(), nil
	}
	if err == repository.ErrCommitNotFound {
		return false, "", nil
	}
	return false, "", err
}

func (m *Module) filesAndCommit(commit *object.Commit, repoFiles []repository.File) ([]File, *Commit, error) {
	var files []File
	mani := &Manifest{}

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

		mani.AddEntry(shake256Hash, file.Name)
	}

	_, maniDigest := mani.Content()

	cmmt := &Commit{
		ModuleId: m.ID,
		OwnerId:  m.OwnerID,
		CommitId: commit.Hash.String()[:32],
		Digest:   maniDigest,
	}

	return files, cmmt, nil
}

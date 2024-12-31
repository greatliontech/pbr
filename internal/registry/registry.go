package registry

import (
	"context"
	"fmt"
	"hash/fnv"

	"github.com/greatliontech/pbr/internal/repository"
	"github.com/greatliontech/pbr/internal/store"
	"github.com/greatliontech/pbr/internal/store/mem"
	"github.com/greatliontech/pbr/pkg/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("pbr.dev/internal/registry")

type Registry struct {
	creds          *repository.CredentialStore
	modules        mem.SyncMap[string, *Module]
	repos          mem.SyncMap[string, *repository.Repository]
	repoCachePath  string
	commitIdModule mem.SyncMap[string, *Module]
	hostName       string
}

func New(mods map[string]*config.Module, creds *repository.CredentialStore, remote, repoCachePath string) *Registry {
	r := &Registry{
		creds:         creds,
		hostName:      remote,
		repoCachePath: repoCachePath,
	}
	return r
}

func (r *Registry) Module(ctx context.Context, org, name string) (*Module, error) {
	ctx, span := tracer.Start(ctx, "Module", trace.WithAttributes(
		attribute.String("org", org),
		attribute.String("name", name),
	))
	defer span.End()

	ownerId, ok := r.owners.Load(org)
	if !ok {
		owner, err := r.stor.GetOwnerByName(ctx, org)
		if err != nil {
			return nil, err
		}
		r.owners.Store(org, owner.ID)
		ownerId = owner.ID
	}

	mod, ok := r.modules.Load(ownerId + "/" + name)
	if !ok {
		modDef, err := r.stor.GetModuleByName(ctx, ownerId, name)
		if err != nil {
			return nil, err
		}
		mod = newModule(r, modDef, r.getRepository(modDef))
		r.modules.Store(ownerId+"/"+name, mod)
	}

	return mod, nil
}

var ErrModuleNotFound = fmt.Errorf("module not found")

func (r *Registry) ModuleByCommitID(ctx context.Context, commitId string) (*Module, error) {
	ctx, span := tracer.Start(ctx, "ModuleByCommitID", trace.WithAttributes(
		attribute.String("commitId", commitId),
	))
	defer span.End()

	// check the cache first
	if mod, ok := r.commitIdModule.Load(commitId); ok {
		return mod, nil
	}

	// get all modules
	mods, err := r.stor.ListModules(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, modDef := range mods {
		mod := newModule(r, modDef, r.getRepository(modDef))
		ok, _, err := mod.HasCommitId(commitId)
		if err != nil {
			return nil, err
		}
		if ok {
			r.commitIdModule.Store(commitId, mod)
			return mod, nil
		}
	}
	return nil, ErrModuleNotFound
}

func (r *Registry) getRepository(mod *store.Module) *repository.Repository {
	var creds repository.AuthProvider
	if r.creds != nil {
		creds = r.creds.AuthProvider(mod.RepoURL)
	}
	repoId := repositoryId(mod.RepoURL)
	if repo, ok := r.repos.Load(repoId); ok {
		return repo
	}
	repoPath := r.repoCachePath + "/" + repoId
	repo := repository.NewRepository(mod.RepoURL, repoPath, creds, mod.Shallow)
	r.repos.Store(repoId, repo)
	return repo
}

func (r *Registry) addToCache(ctx context.Context, commitId, org, name string) error {
	ctx, span := tracer.Start(ctx, "addToCache", trace.WithAttributes(
		attribute.String("commitId", commitId),
		attribute.String("org", org),
		attribute.String("name", name),
	))
	defer span.End()

	if _, ok := r.commitIdModule.Load(commitId); ok {
		return nil
	}

	ownerId, ok := r.owners.Load(org)
	if !ok {
		owner, err := r.stor.GetOwnerByName(ctx, org)
		if err != nil {
			return err
		}
		r.owners.Store(org, owner.ID)
		ownerId = owner.ID
	}
	mod, ok := r.modules.Load(ownerId + "/" + name)
	if !ok {
		modDef, err := r.stor.GetModuleByName(ctx, ownerId, name)
		if err != nil {
			return err
		}
		mod = newModule(r, modDef, r.getRepository(modDef))
		r.modules.Store(ownerId+"/"+name, mod)
	}
	r.commitIdModule.Store(commitId, mod)
	return nil
}

func repositoryId(rmt string) string {
	h := fnv.New128a()
	h.Write([]byte(rmt))
	return fmt.Sprintf("%x", h.Sum(nil))
}

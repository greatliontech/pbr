package registry

import (
	"context"
	"fmt"
	"hash/fnv"

	"github.com/greatliontech/pbr/internal/repository"
	"github.com/greatliontech/pbr/internal/store"
	"github.com/greatliontech/pbr/internal/store/mem"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("pbr.dev/internal/registry")

type Registry struct {
	stor           store.Store
	creds          *repository.CredentialStore
	owners         mem.SyncMap[string, string]
	modules        mem.SyncMap[string, *Module]
	repoCachePath  string
	commitIdModule mem.SyncMap[string, *Module]
}

func New(stor store.Store, creds *repository.CredentialStore, repoCachePath string) *Registry {
	r := &Registry{
		stor:          stor,
		creds:         creds,
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
	}

	mod, ok := r.modules.Load(ownerId + "/" + name)
	if !ok {
		modDef, err := r.stor.GetModuleByName(ctx, ownerId, name)
		if err != nil {
			return nil, err
		}
		mod = NewModule(modDef, r.getRepository(modDef))
		r.modules.Store(ownerId+"/"+name, mod)
	}

	return mod, nil
}

var ErrModuleNotFound = fmt.Errorf("module not found")

func (r *Registry) ModuleByCommitID(ctx context.Context, commitId string) (*Module, error) {
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
		mod := NewModule(modDef, r.getRepository(modDef))
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
	repoPath := r.repoCachePath + "/" + repoId
	return repository.NewRepository(mod.RepoURL, repoPath, creds, mod.Shallow)
}

func repositoryId(rmt string) string {
	h := fnv.New128a()
	h.Write([]byte(rmt))
	return fmt.Sprintf("%x", h.Sum(nil))
}

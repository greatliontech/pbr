package registry

import (
	"context"
	"fmt"
	"hash/fnv"

	"github.com/greatliontech/pbr/internal/repository"
	"github.com/greatliontech/pbr/internal/store"
	"github.com/greatliontech/pbr/internal/store/mem"
)

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

//	func (r *Registry) ModuleByCommitID(ctx context.Context, commitId string) (*Module, error) {
//		// check the cache first
//		if mod, ok := r.commitIdModule.Load(commitId); ok {
//			return mod, nil
//		}
//
//		// get all modules
//		mods, err := r.stor.ListModules(ctx, "")
//		if err != nil {
//			return nil, err
//		}
//		for _, modDef := range mods {
//		}
//	}
func (r *Registry) getRepository(mod *store.Module) *repository.Repository {
	creds := r.creds.AuthProvider(mod.RepoURL)
	repoId := repositoryId(mod.RepoURL)
	repoPath := r.repoCachePath + "/" + repoId
	return repository.NewRepository(mod.RepoURL, repoPath, creds, mod.Shallow)
}

func repositoryId(rmt string) string {
	h := fnv.New128a()
	h.Write([]byte(rmt))
	return fmt.Sprintf("%x", h.Sum(nil))
}

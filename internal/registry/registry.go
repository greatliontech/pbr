package registry

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/greatliontech/pbr/internal/config"
	"github.com/greatliontech/pbr/internal/repository"
	"github.com/greatliontech/pbr/internal/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("pbr.dev/internal/registry")

var ErrModuleNotFound = fmt.Errorf("module not found")

type Registry struct {
	creds          *repository.CredentialStore
	modules        map[string]*Module
	repoCachePath  string
	commitIdModule util.SyncMap[string, *Module]
	hostName       string
}

func New(mods map[string]config.Module, creds *repository.CredentialStore, remote, repoCachePath string) *Registry {
	r := &Registry{
		creds:         creds,
		hostName:      remote,
		repoCachePath: repoCachePath,
		modules:       map[string]*Module{},
	}
	for k, mod := range mods {
		owner := strings.Split(k, "/")[0]
		name := strings.Split(k, "/")[1]
		m := newModule(r, owner, name, mod, r.getRepository(mod))
		r.modules[k] = m
	}
	return r
}

func (r *Registry) Module(ctx context.Context, org, name string) (*Module, error) {
	ctx, span := tracer.Start(ctx, "Registry.Module", trace.WithAttributes(
		attribute.String("org", org),
		attribute.String("name", name),
	))
	defer span.End()

	mod, ok := r.modules[org+"/"+name]
	if !ok {
		return nil, fmt.Errorf("module %s/%s not found", org, name)
	}

	return mod, nil
}

func (r *Registry) ModuleByCommitID(ctx context.Context, commitId string) (*Module, error) {
	ctx, span := tracer.Start(ctx, "Registry.ModuleByCommitID", trace.WithAttributes(
		attribute.String("commitId", commitId),
	))
	defer span.End()

	// check the cache first
	if mod, ok := r.commitIdModule.Load(commitId); ok {
		return mod, nil
	}

	// get all modules
	for _, mod := range r.modules {
		ok, _, err := mod.HasCommitId(ctx, commitId)
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

func (r *Registry) getRepository(mod config.Module) *repository.Repository {
	var creds repository.AuthProvider
	if r.creds != nil {
		creds = r.creds.AuthProvider(mod.Remote)
	}
	repoId := repositoryId(mod.Remote)
	repoPath := r.repoCachePath + "/" + repoId
	repo := repository.NewRepository(mod.Remote, repoPath, creds, mod.Shallow)
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

	mod, ok := r.modules[org+"/"+name]
	if !ok {
		span.RecordError(ErrModuleNotFound)
		span.SetStatus(codes.Error, "module not found")
		return ErrModuleNotFound
	}
	r.commitIdModule.Store(commitId, mod)
	return nil
}

func repositoryId(rmt string) string {
	h := fnv.New128a()
	h.Write([]byte(rmt))
	return fmt.Sprintf("%x", h.Sum(nil))
}

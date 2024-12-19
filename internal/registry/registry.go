package registry

import (
	"fmt"
	"hash/fnv"
	"path/filepath"
	"strings"

	"github.com/greatliontech/pbr/internal/repository"
	"github.com/greatliontech/pbr/pkg/config"
)

type Registry struct {
	config  *config.Config
	modules map[string]*Module
}

func New(conf *config.Config) (*Registry, error) {
	r := &Registry{
		config:  conf,
		modules: make(map[string]*Module),
	}

	credStore, err := repository.NewCredentialStore(conf.Credentials.Git)
	if err != nil {
		return nil, fmt.Errorf("failed to create credential store: %w", err)
	}

	for k, v := range conf.Modules {
		data := strings.Split(k, "/")
		org := data[0]
		name := data[1]
		auth, err := credStore.Auth(k)
		if err != nil {
			return nil, fmt.Errorf("failed to get credentials for %s: %w", k, err)
		}
		repoId := repositoryId(v.Remote)
		path := filepath.Join(conf.CacheDir, repoId)
		repo := repository.NewRepository(v.Remote, path, auth, v.Shallow)
		mod, err := NewModule(org, name, repo, v.Path, v.Filters)
		if err != nil {
			return nil, fmt.Errorf("failed to create module %s/%s: %w", org, name, err)
		}
		r.modules[k] = mod
	}

	return r, nil
}

func (r *Registry) Module(org, name string) {
}

func repositoryId(rmt string) string {
	h := fnv.New128a()
	h.Write([]byte(rmt))
	return fmt.Sprintf("%x", h.Sum(nil))
}

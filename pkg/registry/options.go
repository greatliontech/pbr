package registry

import (
	"crypto/tls"

	"github.com/greatliontech/pbr/internal/repository"
	"github.com/greatliontech/pbr/internal/store"
	"github.com/greatliontech/pbr/pkg/codegen"
	"github.com/greatliontech/pbr/pkg/config"
)

type Option func(*Registry)

func WithTLSCert(cert *tls.Certificate) Option {
	return func(r *Registry) {
		r.cert = cert
	}
}

func WithRepoCredStore(creds *repository.CredentialStore) Option {
	return func(r *Registry) {
		r.repoCreds = creds
	}
}

func WithAddress(addr string) Option {
	return func(r *Registry) {
		r.addr = addr
	}
}

func WithModules(mods map[string]config.Module) Option {
	return func(r *Registry) {
		r.modules = mods
	}
}

func WithPlugins(plugins map[string]config.Plugin) Option {
	return func(r *Registry) {
		r.plugins = make(map[string]*codegen.Plugin)
		for k, v := range plugins {
			r.plugins[k] = codegen.NewPlugin(r.ofs, v.Image)
		}
	}
}

func WithCacheDir(cacheDir string) Option {
	return func(r *Registry) {
		r.cacheDir = cacheDir
	}
}

func WithStore(s store.Store) Option {
	return func(r *Registry) {
		r.stor = s
	}
}

func WithAdminToken(token string) Option {
	return func(r *Registry) {
		r.adminToken = token
	}
}

func WithUsers(users map[string]string) Option {
	return func(r *Registry) {
		r.users = users
		for k, v := range users {
			r.tokens[v] = k
		}
	}
}

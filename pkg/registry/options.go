package registry

import (
	"crypto/tls"

	"github.com/gobwas/glob"
	"github.com/greatliontech/pbr/pkg/codegen"
	"github.com/greatliontech/pbr/pkg/config"
	"github.com/greatliontech/pbr/pkg/repository"
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

func WithModules(mods map[glob.Glob]config.Module) Option {
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

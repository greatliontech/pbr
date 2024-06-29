package registry

import (
	"crypto/tls"
	"net/http"

	"buf.build/gen/go/bufbuild/buf/connectrpc/go/buf/alpha/registry/v1alpha1/registryv1alpha1connect"
	"connectrpc.com/connect"
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

func WithBSRCreds(creds map[string]string) Option {
	return func(r *Registry) {
		r.bsrRemotes = make(map[string]registryv1alpha1connect.ResolveServiceClient)
		for k, v := range creds {
			r.bsrRemotes[k] = registryv1alpha1connect.NewResolveServiceClient(
				http.DefaultClient,
				"https://"+k,
				connect.WithInterceptors(newAuthInterceptor(v)),
			)
		}
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

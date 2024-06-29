package registry

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	"buf.build/gen/go/bufbuild/buf/connectrpc/go/buf/alpha/registry/v1alpha1/registryv1alpha1connect"
	"connectrpc.com/connect"
	"github.com/greatliontech/ocifs"
	"github.com/greatliontech/pbr/pkg/codegen"
	"github.com/greatliontech/pbr/pkg/config"
	"github.com/greatliontech/pbr/pkg/repository"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type Registry struct {
	ofs        *ocifs.OCIFS
	modules    map[string]config.Module
	plugins    map[string]*codegen.Plugin
	bsrRemotes map[string]registryv1alpha1connect.ResolveServiceClient
	repos      map[string]*repository.Repository
	server     *http.Server
	cert       *tls.Certificate
	repoCreds  *repository.CredentialStore
	hostName   string
	addr       string
}

func New(hostName string, opts ...Option) (*Registry, error) {
	reg := &Registry{
		addr:     ":443",
		hostName: hostName,
	}

	// init ocifs
	ofs, err := ocifs.New(ocifs.WithExtraDirs([]string{"/proc", "/sys"}))
	if err != nil {
		return nil, err
	}
	reg.ofs = ofs

	// apply options
	for _, o := range opts {
		o(reg)
	}

	mux := http.NewServeMux()

	mux.Handle(registryv1alpha1connect.NewResolveServiceHandler(reg))
	mux.Handle(registryv1alpha1connect.NewDownloadServiceHandler(reg))
	mux.Handle(registryv1alpha1connect.NewCodeGenerationServiceHandler(reg))
	mux.Handle(registryv1alpha1connect.NewRepositoryServiceHandler(reg))

	reg.server = &http.Server{
		Addr:         reg.addr,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		Handler:      mux,
	}

	if reg.cert != nil {
		reg.server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{*reg.cert}}
	}

	return reg, nil
}

func (reg *Registry) Serve() error {
	if reg.cert != nil {
		reg.server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{*reg.cert}}
		if err := http2.ConfigureServer(reg.server, nil); err != nil {
			return err
		}
		return reg.server.ListenAndServeTLS("", "")
	}
	h2s := &http2.Server{}
	handler := h2c.NewHandler(reg.server.Handler, h2s)
	reg.server.Handler = handler
	return reg.server.ListenAndServe()
}

func (reg *Registry) getRepository(ctx context.Context, owner, repo string) (*repository.Repository, error) {
	if reg.repos == nil {
		reg.repos = map[string]*repository.Repository{}
	}
	key := owner + "/" + repo
	if reg.repos[key] == nil {
		mod, ok := reg.modules[key]
		if !ok {
			return nil, fmt.Errorf("module not found for %s", key)
		}
		target := strings.TrimSuffix(mod.Remote, "/") + "/" + owner + "/" + repo
		if mod.Replace {
			target = mod.Remote
		}
		repoOpts := []repository.Option{}
		if reg.repoCreds != nil {
			auth := reg.repoCreds.Auth(target)
			if auth != nil {
				repoOpts = append(repoOpts, repository.WithAuth(auth))
			}
		}
		if mod.Path != "" {
			repoOpts = append(repoOpts, repository.WithRoot(mod.Path))
		}
		if mod.Filters != nil {
			repoOpts = append(repoOpts, repository.WithFilters(mod.Filters))
		}
		repo, err := repository.New(target, repoOpts...)
		if err != nil {
			return nil, err
		}
		reg.repos[key] = repo
	}
	return reg.repos[key], nil
}

const (
	authenticationHeader      = "Authorization"
	authenticationTokenPrefix = "Bearer "
)

func newAuthInterceptor(token string) connect.UnaryInterceptorFunc {
	interceptor := func(next connect.UnaryFunc) connect.UnaryFunc {
		return connect.UnaryFunc(func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set(authenticationHeader, authenticationTokenPrefix+token)
			return next(ctx, req)
		})
	}
	return connect.UnaryInterceptorFunc(interceptor)
}

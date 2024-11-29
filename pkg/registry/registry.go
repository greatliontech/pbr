package registry

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"text/template"
	"time"

	"buf.build/gen/go/bufbuild/buf/connectrpc/go/buf/alpha/registry/v1alpha1/registryv1alpha1connect"
	"buf.build/gen/go/bufbuild/registry/connectrpc/go/buf/registry/module/v1beta1/modulev1beta1connect"
	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
	"github.com/gobwas/glob"
	"github.com/greatliontech/ocifs"
	"github.com/greatliontech/pbr/pkg/codegen"
	"github.com/greatliontech/pbr/pkg/config"
	"github.com/greatliontech/pbr/pkg/repository"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type Module struct {
	Match glob.Glob
	Mod   config.Module
}

type Registry struct {
	registryv1alpha1connect.UnimplementedResolveServiceHandler
	registryv1alpha1connect.UnimplementedDownloadServiceHandler
	registryv1alpha1connect.UnimplementedCodeGenerationServiceHandler
	registryv1alpha1connect.UnimplementedRepositoryServiceHandler
	ofs        *ocifs.OCIFS
	modules    []Module
	plugins    map[string]*codegen.Plugin
	bsrRemotes map[string]registryv1alpha1connect.ResolveServiceClient
	repos      map[string]*repository.Repository
	server     *http.Server
	cert       *tls.Certificate
	repoCreds  *repository.CredentialStore
	hostName   string
	addr       string
	commits    map[string]*v1beta1.Commit
}

func New(hostName string, opts ...Option) (*Registry, error) {
	reg := &Registry{
		addr:     ":443",
		hostName: hostName,
		repos:    map[string]*repository.Repository{},
	}

	// init ocifs
	ofs, err := ocifs.New()
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
	mux.Handle(modulev1beta1connect.NewCommitServiceHandler(reg))
	mux.Handle(modulev1beta1connect.NewGraphServiceHandler(reg))

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

type tplContext struct {
	Remote     string
	Owner      string
	Repository string
}

func (reg *Registry) getRepository(ctx context.Context, owner, repo string) (*repository.Repository, error) {
	key := owner + "/" + repo
	if reg.repos[key] == nil {
		mod, ok := reg.getModule(owner, repo)
		if !ok {
			return nil, fmt.Errorf("module not found for %s", key)
		}
		format := "{{.Remote}}/{{.Owner}}/{{.Repository}}"
		if mod.Format != "" {
			format = mod.Format
		}
		target, err := formatTarget(format, tplContext{
			Remote:     strings.TrimSuffix(mod.Remote, "/"),
			Owner:      owner,
			Repository: repo,
		})
		if err != nil {
			return nil, err
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

func (reg *Registry) getModule(owner, repo string) (config.Module, bool) {
	key := owner + "/" + repo
	for _, mod := range reg.modules {
		if mod.Match.Match(key) {
			return mod.Mod, true
		}
	}
	return config.Module{}, false
}

func formatTarget(format string, tplCtx tplContext) (string, error) {
	tpl, err := template.New("").Parse(format)
	if err != nil {
		return "", err
	}
	out := &bytes.Buffer{}
	if err := tpl.Execute(out, tplCtx); err != nil {
		return "", nil
	}
	return out.String(), nil
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

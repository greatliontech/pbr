package registry

import (
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"text/template"
	"time"

	"buf.build/gen/go/bufbuild/buf/connectrpc/go/buf/alpha/registry/v1alpha1/registryv1alpha1connect"
	"buf.build/gen/go/bufbuild/registry/connectrpc/go/buf/registry/module/v1/modulev1connect"
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

type internalModule struct {
	Owner  string
	Module string
	Repo   string
}

type Registry struct {
	registryv1alpha1connect.UnimplementedCodeGenerationServiceHandler
	ofs            *ocifs.OCIFS
	modules        []Module
	plugins        map[string]*codegen.Plugin
	bsrRemotes     map[string]registryv1alpha1connect.ResolveServiceClient
	repos          map[string]*repository.Repository
	server         *http.Server
	cert           *tls.Certificate
	repoCreds      *repository.CredentialStore
	hostName       string
	addr           string
	commits        map[string]*v1beta1.Commit
	commitHashes   map[string]string
	moduleIds      map[string]*internalModule
	commitToModule map[string]*internalModule
}

func New(hostName string, opts ...Option) (*Registry, error) {
	reg := &Registry{
		addr:           ":443",
		hostName:       hostName,
		repos:          map[string]*repository.Repository{},
		commits:        map[string]*v1beta1.Commit{},
		commitHashes:   map[string]string{},
		moduleIds:      map[string]*internalModule{},
		commitToModule: map[string]*internalModule{},
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

	interceptors := connect.WithInterceptors(newAuthInterceptor("verySecure42"))

	mux.Handle(registryv1alpha1connect.NewCodeGenerationServiceHandler(reg, interceptors))
	mux.Handle(modulev1beta1connect.NewCommitServiceHandler(reg, interceptors))
	mux.Handle(modulev1beta1connect.NewGraphServiceHandler(reg, interceptors))
	mux.Handle(modulev1beta1connect.NewDownloadServiceHandler(reg, interceptors))
	mux.Handle(modulev1connect.NewModuleServiceHandler(reg, interceptors))

	mux.Handle("/readyz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ready")
	}))
	mux.Handle("/livez", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "live")
	}))

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
			auth, err := reg.repoCreds.Auth(target)
			if err != nil {
				return nil, err
			}
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
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			hdr := req.Header().Get(authenticationHeader)
			if hdr == "" {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("no token provided"))
			}

			if !strings.HasPrefix(hdr, authenticationTokenPrefix) {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid auth header"))
			}

			tokenString := strings.TrimSpace(strings.TrimPrefix(hdr, authenticationTokenPrefix))
			if tokenString == "" {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("token missing"))
			}

			if subtle.ConstantTimeCompare([]byte(tokenString), []byte(token)) != 1 {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid token"))
			}

			return next(ctx, req)
		}
	}
}

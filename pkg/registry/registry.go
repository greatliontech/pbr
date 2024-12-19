package registry

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"path/filepath"
	"strings"

	"buf.build/gen/go/bufbuild/buf/connectrpc/go/buf/alpha/registry/v1alpha1/registryv1alpha1connect"
	"buf.build/gen/go/bufbuild/registry/connectrpc/go/buf/registry/module/v1/modulev1connect"
	"buf.build/gen/go/bufbuild/registry/connectrpc/go/buf/registry/module/v1beta1/modulev1beta1connect"
	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
	"github.com/gobwas/glob"
	"github.com/greatliontech/ocifs"
	"github.com/greatliontech/pbr/internal/module"
	"github.com/greatliontech/pbr/internal/repository"
	"github.com/greatliontech/pbr/pkg/codegen"
	"github.com/greatliontech/pbr/pkg/config"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type internalModule struct {
	Owner  string
	Module string
	Repo   string
}

type Registry struct {
	registryv1alpha1connect.UnimplementedCodeGenerationServiceHandler
	ofs            *ocifs.OCIFS
	modules        map[string]config.Module
	plugins        map[string]*codegen.Plugin
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
	cacheDir       string
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
		modules:        map[string]config.Module{},
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
		Addr:    reg.addr,
		Handler: mux,
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

func (reg *Registry) getModule(owner, modl string) (*module.Module, error) {
	key := owner + "/" + modl
	modConf, ok := reg.modules[key]
	if !ok {
		return nil, fmt.Errorf("module not found for %s", key)
	}
	target := modConf.Remote
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
	if modConf.Shallow {
		repoOpts = append(repoOpts, repository.WithShallow())
	}
	repoNm, err := repoName(target)
	if err != nil {
		return nil, err
	}
	repoPath := filepath.Join(reg.cacheDir, repoNm)
	repo := repository.NewRepository(target, repoPath, repoOpts...)
	filters := []glob.Glob{}
	for _, fltr := range modConf.Filters {
		filter, err := glob.Compile(fltr)
		if err != nil {
			return nil, err
		}
		filters = append(filters, filter)
	}
	mod := module.New(owner, modl, repo, modConf.Path, filters)
	return mod, nil
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

func repoName(rmt string) (string, error) {
	h := fnv.New128a()
	_, err := h.Write([]byte(rmt))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

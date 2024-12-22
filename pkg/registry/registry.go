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
	"buf.build/gen/go/bufbuild/registry/connectrpc/go/buf/registry/owner/v1/ownerv1connect"
	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/greatliontech/ocifs"
	"github.com/greatliontech/pbr/internal/registry"
	"github.com/greatliontech/pbr/internal/repository"
	"github.com/greatliontech/pbr/internal/store"
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
	stor           store.Store
	adminToken     string
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

	interceptors := connect.WithInterceptors(newAuthInterceptor(reg.adminToken))

	mux.Handle(registryv1alpha1connect.NewCodeGenerationServiceHandler(reg, interceptors))
	mux.Handle(modulev1beta1connect.NewCommitServiceHandler(reg, interceptors))
	mux.Handle(modulev1beta1connect.NewGraphServiceHandler(reg, interceptors))
	mux.Handle(modulev1beta1connect.NewDownloadServiceHandler(reg, interceptors))
	mux.Handle(modulev1connect.NewModuleServiceHandler(reg, interceptors))
	mux.Handle(ownerv1connect.NewOwnerServiceHandler(reg, interceptors))

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

func (reg *Registry) getModule(owner, modl string) (*registry.Module, error) {
	key := owner + "/" + modl
	modConf, ok := reg.modules[key]
	if !ok {
		return nil, fmt.Errorf("module not found for %s", key)
	}
	target := modConf.Remote
	var err error
	var auth transport.AuthMethod
	if reg.repoCreds != nil {
		auth, err = reg.repoCreds.Auth(target)
		if err != nil {
			return nil, err
		}
	}
	repoNm := repoName(target)
	repoPath := filepath.Join(reg.cacheDir, repoNm)
	repo := repository.NewRepository(target, repoPath, auth, modConf.Shallow)
	return registry.NewModule(owner, modl, repo, modConf.Path, modConf.Filters)
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

func repoName(rmt string) string {
	h := fnv.New128a()
	h.Write([]byte(rmt))
	return fmt.Sprintf("%x", h.Sum(nil))
}

package registry

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"net/http"
	"path/filepath"
	"strings"

	"buf.build/gen/go/bufbuild/buf/connectrpc/go/buf/alpha/registry/v1alpha1/registryv1alpha1connect"
	"buf.build/gen/go/bufbuild/registry/connectrpc/go/buf/registry/module/v1/modulev1connect"
	"buf.build/gen/go/bufbuild/registry/connectrpc/go/buf/registry/module/v1beta1/modulev1beta1connect"
	"buf.build/gen/go/bufbuild/registry/connectrpc/go/buf/registry/owner/v1/ownerv1connect"
	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/greatliontech/ocifs"
	"github.com/greatliontech/pbr/internal/registry"
	"github.com/greatliontech/pbr/internal/repository"
	"github.com/greatliontech/pbr/internal/store"
	"github.com/greatliontech/pbr/pkg/codegen"
	"github.com/greatliontech/pbr/pkg/config"
	"go.opentelemetry.io/otel"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

var tracer = otel.Tracer("pbr.dev/pkg/registry")

type internalModule struct {
	Owner  string
	Module string
	Repo   string
}

type Registry struct {
	registryv1alpha1connect.UnimplementedCodeGenerationServiceHandler
	stor           store.Store
	moduleIds      map[string]*internalModule
	commitHashes   map[string]string
	repos          map[string]*repository.Repository
	server         *http.Server
	cert           *tls.Certificate
	repoCreds      *repository.CredentialStore
	tokens         map[string]string
	users          map[string]string
	commits        map[string]*v1beta1.Commit
	pluginsConf    map[string]config.Plugin
	plugins        map[string]*codegen.Plugin
	modules        map[string]config.Module
	commitToModule map[string]*internalModule
	ofs            *ocifs.OCIFS
	cacheDir       string
	adminToken     string
	addr           string
	hostName       string
	regCreds       map[string]authn.AuthConfig
	reg            *registry.Registry
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
		users:          map[string]string{},
		tokens:         map[string]string{},
		regCreds:       map[string]authn.AuthConfig{},
		pluginsConf:    map[string]config.Plugin{},
		plugins:        map[string]*codegen.Plugin{},
	}

	// apply options
	for _, o := range opts {
		o(reg)
	}

	// ocifs options
	ofsOpts := []ocifs.Option{}
	if len(reg.regCreds) > 0 {
		for k, v := range reg.regCreds {
			ofsOpts = append(ofsOpts, ocifs.WithAuthSource(k, v))
			fmt.Printf("auth source: %s\n", k)
		}
	}

	// init ocifs
	ofs, err := ocifs.New(ofsOpts...)
	if err != nil {
		return nil, err
	}
	reg.ofs = ofs

	for k, v := range reg.pluginsConf {
		reg.plugins[k] = codegen.NewPlugin(ofs, v.Image, v.Default)
	}

	if reg.adminToken != "" {
		reg.users["admin"] = reg.adminToken
		reg.tokens[reg.adminToken] = "admin"
	}

	reg.reg = registry.New(reg.stor, reg.repoCreds, reg.cacheDir)

	mux := http.NewServeMux()

	otelInt, err := otelconnect.NewInterceptor()
	if err != nil {
		return nil, err
	}

	interceptors := connect.WithInterceptors(
		newAuthInterceptor(reg.tokens),
		otelInt,
	)

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

func (reg *Registry) Serve(ctx context.Context) error {
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
	reg.server.BaseContext = func(listener net.Listener) context.Context {
		return ctx
	}
	return reg.server.ListenAndServe()
}

func (reg *Registry) Shutdown(ctx context.Context) error {
	return reg.server.Shutdown(ctx)
}

func (reg *Registry) getModule(owner, modl string) (*registry.Module, error) {
	key := owner + "/" + modl
	modConf, ok := reg.modules[key]
	if !ok {
		return nil, fmt.Errorf("module not found for %s", key)
	}
	target := modConf.Remote
	var auth repository.AuthProvider
	if reg.repoCreds != nil {
		auth = reg.repoCreds.AuthProvider(target)
	}
	repoNm := repoName(target)
	repoPath := filepath.Join(reg.cacheDir, repoNm)
	repo := repository.NewRepository(target, repoPath, auth, modConf.Shallow)
	mod := &store.Module{
		OwnerID: fakeUUID(owner),
		Name:    modl,
		RepoURL: modConf.Remote,
		Root:    modConf.Path,
		Shallow: modConf.Shallow,
	}
	mod.ID = fakeUUID(mod.OwnerID + "/" + mod.Name)
	return registry.NewModule(mod, repo), nil
}

const (
	authenticationHeader      = "Authorization"
	authenticationTokenPrefix = "Bearer "
)

func newAuthInterceptor(tokens map[string]string) connect.UnaryInterceptorFunc {
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

			user, ok := tokens[tokenString]
			if !ok {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid token"))
			}

			ctx = context.WithValue(ctx, "user", user)

			return next(ctx, req)
		}
	}
}

func repoName(rmt string) string {
	h := fnv.New128a()
	h.Write([]byte(rmt))
	return fmt.Sprintf("%x", h.Sum(nil))
}

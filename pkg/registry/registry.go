package registry

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"buf.build/gen/go/bufbuild/buf/connectrpc/go/buf/alpha/registry/v1alpha1/registryv1alpha1connect"
	"buf.build/gen/go/bufbuild/registry/connectrpc/go/buf/registry/module/v1/modulev1connect"
	"buf.build/gen/go/bufbuild/registry/connectrpc/go/buf/registry/module/v1beta1/modulev1beta1connect"
	"buf.build/gen/go/bufbuild/registry/connectrpc/go/buf/registry/owner/v1/ownerv1connect"
	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/greatliontech/ocifs"
	"github.com/greatliontech/pbr/internal/registry"
	"github.com/greatliontech/pbr/internal/repository"
	"github.com/greatliontech/pbr/internal/util"
	"github.com/greatliontech/pbr/pkg/codegen"
	"github.com/greatliontech/pbr/pkg/config"
	"go.opentelemetry.io/otel"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

var tracer = otel.Tracer("pbr.dev/pkg/registry")

type Registry struct {
	registryv1alpha1connect.UnimplementedCodeGenerationServiceHandler
	conf         *config.Config
	commitHashes map[string]string
	repos        map[string]*repository.Repository
	server       *http.Server
	cert         *tls.Certificate
	repoCreds    *repository.CredentialStore
	tokens       map[string]string
	users        map[string]string
	plugins      map[string]*codegen.Plugin
	ofs          *ocifs.OCIFS
	regCreds     map[string]authn.AuthConfig
	reg          *registry.Registry
	ownerIds     map[string]string
	moduleIds    map[string]string
}

func New(c *config.Config) (*Registry, error) {
	reg := &Registry{
		conf:     c,
		repos:    map[string]*repository.Repository{},
		tokens:   map[string]string{},
		users:    map[string]string{},
		regCreds: map[string]authn.AuthConfig{},
		plugins:  map[string]*codegen.Plugin{},
	}

	for k := range c.Modules {
		ownerName := strings.Split(k, "/")[0]
		modName := strings.Split(k, "/")[1]
		ownerId := util.OwnerID(ownerName)
		reg.ownerIds[ownerName] = ownerId
		modId := util.ModuleID(ownerId, modName)
		reg.moduleIds[modId] = ownerId + "/" + modName
	}

	if reg.conf.Address == "" {
		reg.conf.Address = ":443"
	}

	if c.Credentials.Git != nil {
		credStore, err := repository.NewCredentialStore(c.Credentials.Git)
		if err != nil {
			slog.Error("Failed to create git credential store", "err", err)
			return nil, err
		}
		reg.repoCreds = credStore
	}

	if c.Credentials.ContainerRegistry != nil {
		regCreds := map[string]authn.AuthConfig{}
		for k, v := range c.Credentials.ContainerRegistry {
			regCreds[k] = authn.AuthConfig(v)
		}
		reg.regCreds = regCreds
	}

	if c.Users != nil {
		for k, v := range c.Users {
			reg.users[k] = v
		}
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

	for k, v := range reg.conf.Plugins {
		reg.plugins[k] = codegen.NewPlugin(ofs, v.Image, v.Default)
	}

	if reg.conf.AdminToken != "" {
		reg.users["admin"] = reg.conf.AdminToken
		reg.tokens[reg.conf.AdminToken] = "admin"
	}

	reg.reg = registry.New(reg.conf.Modules, reg.repoCreds, reg.conf.Host, reg.conf.CacheDir)

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
		Addr:    reg.conf.Address,
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

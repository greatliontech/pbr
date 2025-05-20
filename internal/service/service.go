package service

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
	"github.com/greatliontech/pbr/internal/codegen"
	"github.com/greatliontech/pbr/internal/config"
	"github.com/greatliontech/pbr/internal/registry"
	"github.com/greatliontech/pbr/internal/repository"
	"github.com/greatliontech/pbr/internal/util"
	"go.opentelemetry.io/otel"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

var tracer = otel.Tracer("pbr.dev/internal/service")

type contextKey string

const userContextKey contextKey = "user"

func contextWithUser(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

func userFromContext(ctx context.Context) string {
	return ctx.Value(userContextKey).(string)
}

type Service struct {
	registryv1alpha1connect.UnimplementedCodeGenerationServiceHandler
	conf      *config.Config
	server    *http.Server
	cert      *tls.Certificate
	repoCreds *repository.CredentialStore
	tokens    map[string]string
	users     map[string]string
	plugins   map[string]*codegen.Plugin
	ofs       *ocifs.OCIFS
	regCreds  map[string]authn.AuthConfig
	reg       *registry.Registry
	ownerIds  map[string]string
	moduleIds map[string]string
}

func New(c *config.Config) (*Service, error) {
	svc := &Service{
		conf:      c,
		tokens:    map[string]string{},
		users:     map[string]string{},
		regCreds:  map[string]authn.AuthConfig{},
		plugins:   map[string]*codegen.Plugin{},
		ownerIds:  map[string]string{},
		moduleIds: map[string]string{},
	}

	for k := range c.Modules {
		ownerName := strings.Split(k, "/")[0]
		modName := strings.Split(k, "/")[1]
		ownerId := util.OwnerID(ownerName)
		svc.ownerIds[ownerId] = ownerName
		modId := util.ModuleID(ownerId, modName)
		svc.moduleIds[modId] = ownerId + "/" + modName
		slog.Debug("parsing modules", "ownerId", ownerId, "owner", ownerName, "id", modId, "module", modName)
	}

	if svc.conf.Address == "" {
		svc.conf.Address = ":443"
	}

	if c.Credentials.Git != nil {
		credStore, err := repository.NewCredentialStore(c.Credentials.Git)
		if err != nil {
			slog.Error("Failed to create git credential store", "err", err)
			return nil, err
		}
		svc.repoCreds = credStore
	}

	if c.Credentials.ContainerRegistry != nil {
		regCreds := map[string]authn.AuthConfig{}
		for k, v := range c.Credentials.ContainerRegistry {
			regCreds[k] = authn.AuthConfig(v)
		}
		svc.regCreds = regCreds
	}

	if c.Users != nil {
		for k, v := range c.Users {
			svc.users[k] = v
		}
	}

	// ocifs options
	ofsOpts := []ocifs.Option{}
	if len(svc.regCreds) > 0 {
		for k, v := range svc.regCreds {
			ofsOpts = append(ofsOpts, ocifs.WithAuthSource(k, v))
			fmt.Printf("auth source: %s\n", k)
		}
	}

	// init ocifs
	ofs, err := ocifs.New(ofsOpts...)
	if err != nil {
		return nil, err
	}
	svc.ofs = ofs

	for k, v := range svc.conf.Plugins {
		svc.plugins[k] = codegen.NewPlugin(ofs, v.Image, v.Default)
	}

	if svc.conf.AdminToken != "" {
		svc.users["admin"] = svc.conf.AdminToken
		svc.tokens[svc.conf.AdminToken] = "admin"
	}

	for k, v := range svc.users {
		svc.tokens[v] = k
	}

	svc.reg = registry.New(svc.conf.Modules, svc.repoCreds, svc.conf.Host, svc.conf.CacheDir)

	mux := http.NewServeMux()

	intcptrs := []connect.Interceptor{}

	otelInt, err := otelconnect.NewInterceptor()
	if err != nil {
		return nil, err
	}
	intcptrs = append(intcptrs, otelInt)

	if !c.NoLogin {
		intcptrs = append(intcptrs, newAuthInterceptor(svc.tokens))
	}

	interceptors := connect.WithInterceptors(intcptrs...)

	mux.Handle(registryv1alpha1connect.NewCodeGenerationServiceHandler(svc, interceptors))
	mux.Handle(modulev1beta1connect.NewCommitServiceHandler(svc, interceptors))
	mux.Handle(modulev1beta1connect.NewGraphServiceHandler(svc, interceptors))
	mux.Handle(modulev1beta1connect.NewDownloadServiceHandler(svc, interceptors))
	mux.Handle(modulev1connect.NewModuleServiceHandler(svc, interceptors))
	mux.Handle(ownerv1connect.NewOwnerServiceHandler(svc, interceptors))

	mux.Handle("/readyz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ready")
	}))
	mux.Handle("/livez", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "live")
	}))

	svc.server = &http.Server{
		Addr:    svc.conf.Address,
		Handler: mux,
	}

	if svc.cert != nil {
		svc.server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{*svc.cert}}
	}

	return svc, nil
}

func (svc *Service) Serve(ctx context.Context) error {
	if svc.cert != nil {
		svc.server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{*svc.cert}}
		if err := http2.ConfigureServer(svc.server, nil); err != nil {
			return err
		}
		return svc.server.ListenAndServeTLS("", "")
	}
	h2s := &http2.Server{}
	handler := h2c.NewHandler(svc.server.Handler, h2s)
	svc.server.Handler = handler
	svc.server.BaseContext = func(listener net.Listener) context.Context {
		return ctx
	}
	return svc.server.ListenAndServe()
}

func (svc *Service) Shutdown(ctx context.Context) error {
	return svc.server.Shutdown(ctx)
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

			ctx = contextWithUser(ctx, user)

			return next(ctx, req)
		}
	}
}

func repoName(rmt string) string {
	h := fnv.New128a()
	h.Write([]byte(rmt))
	return fmt.Sprintf("%x", h.Sum(nil))
}

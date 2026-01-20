package service

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
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
	"github.com/greatliontech/pbr/internal/registry/cas"
	"github.com/greatliontech/pbr/internal/storage/filesystem"
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
	if user, ok := ctx.Value(userContextKey).(string); ok {
		return user
	}
	return ""
}

type Service struct {
	registryv1alpha1connect.UnimplementedCodeGenerationServiceHandler
	conf     *config.Config
	server   *http.Server
	cert     *tls.Certificate
	tokens   map[string]string
	users    map[string]string
	plugins  map[string]*codegen.Plugin
	ofs      *ocifs.OCIFS
	regCreds map[string]authn.AuthConfig
	casReg   *cas.Registry
}

func New(c *config.Config) (*Service, error) {
	// CAS storage is required
	if c.CacheDir == "" {
		return nil, fmt.Errorf("cache_dir is required for CAS storage")
	}

	svc := &Service{
		conf:     c,
		tokens:   map[string]string{},
		users:    map[string]string{},
		regCreds: map[string]authn.AuthConfig{},
		plugins:  map[string]*codegen.Plugin{},
	}

	if svc.conf.Address == "" {
		svc.conf.Address = ":443"
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
			slog.Debug("auth source configured", "registry", k)
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

	// Load TLS certificate if configured (from files or PEM strings)
	if c.TLS != nil {
		cert, err := loadTLSCert(c.TLS)
		if err != nil {
			return nil, err
		}
		if cert != nil {
			svc.cert = cert
		}
	}

	// Initialize CAS storage
	storagePath := c.CacheDir + "/cas"
	blobStore := filesystem.NewBlobStore(storagePath + "/blobs")
	manifestStore := filesystem.NewManifestStore(storagePath + "/manifests")
	metadataStore := filesystem.NewMetadataStore(storagePath + "/metadata")
	svc.casReg = cas.New(blobStore, manifestStore, metadataStore, c.Host)
	slog.Info("CAS storage initialized", "path", storagePath)

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
	mux.Handle(modulev1beta1connect.NewUploadServiceHandler(svc, interceptors))
	mux.Handle(modulev1beta1connect.NewModuleServiceHandler(svc, interceptors))
	mux.Handle(modulev1connect.NewModuleServiceHandler(NewModuleServiceV1(svc), interceptors))
	mux.Handle(modulev1connect.NewUploadServiceHandler(NewUploadServiceV1(svc), interceptors))
	mux.Handle(modulev1connect.NewDownloadServiceHandler(NewDownloadServiceV1(svc), interceptors))
	mux.Handle(modulev1connect.NewCommitServiceHandler(NewCommitServiceV1(svc), interceptors))
	mux.Handle(modulev1connect.NewGraphServiceHandler(NewGraphServiceV1(svc), interceptors))
	mux.Handle(modulev1connect.NewResourceServiceHandler(NewResourceServiceV1(svc), interceptors))
	mux.Handle(modulev1connect.NewLabelServiceHandler(NewLabelServiceV1(svc), interceptors))
	mux.Handle(ownerv1connect.NewOwnerServiceHandler(svc, interceptors))

	mux.Handle("/readyz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ready")
	}))
	mux.Handle("/livez", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "live")
	}))

	var handler http.Handler = mux
	// Debug middleware - enable with PBR_DEBUG_HTTP=1
	if os.Getenv("PBR_DEBUG_HTTP") == "1" {
		handler = debugMiddleware(mux)
		slog.Info("Debug HTTP logging enabled")
	}

	svc.server = &http.Server{
		Addr:    svc.conf.Address,
		Handler: handler,
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

func loadTLSCert(tlsConf *config.TLS) (*tls.Certificate, error) {
	switch {
	case tlsConf.CertFile != "" && tlsConf.KeyFile != "":
		// Load from files
		cert, err := tls.LoadX509KeyPair(tlsConf.CertFile, tlsConf.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS certificate from files: %w", err)
		}
		slog.Info("TLS enabled", "cert", tlsConf.CertFile)
		return &cert, nil
	case tlsConf.CertPEM != "" && tlsConf.KeyPEM != "":
		// Load from PEM strings (e.g., from env vars via ${TLS_CERT})
		cert, err := tls.X509KeyPair([]byte(tlsConf.CertPEM), []byte(tlsConf.KeyPEM))
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS certificate from PEM: %w", err)
		}
		slog.Info("TLS enabled from PEM")
		return &cert, nil
	default:
		// Incomplete TLS config
		return nil, nil
	}
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

// debugMiddleware logs all HTTP requests for debugging.
func debugMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read body for logging (if small enough)
		var bodyPreview string
		if r.Body != nil && r.ContentLength > 0 && r.ContentLength < 1024 {
			body, err := io.ReadAll(r.Body)
			if err == nil {
				bodyPreview = string(body)
				// Restore body for handler
				r.Body = io.NopCloser(strings.NewReader(string(body)))
			}
		}

		slog.Debug("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"content-type", r.Header.Get("Content-Type"),
			"content-length", r.ContentLength,
			"body-preview", bodyPreview,
		)

		// Wrap response writer to capture status
		wrapped := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(wrapped, r)

		slog.Debug("HTTP response",
			"path", r.URL.Path,
			"status", wrapped.status,
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

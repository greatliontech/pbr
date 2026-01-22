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
	"sync"
	"time"

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
	"github.com/greatliontech/pbr/internal/storage"
	"go.opentelemetry.io/otel"
	"gocloud.dev/blob"
	_ "gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/gcsblob"
	"gocloud.dev/docstore"
	_ "gocloud.dev/docstore/gcpfirestore"
	"gocloud.dev/docstore/memdocstore"
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

// tokenInfo holds information about an authentication token.
type tokenInfo struct {
	Username  string
	ExpiresAt time.Time // Zero value means never expires (for static tokens)
}

// IsExpired returns true if the token has expired.
func (t *tokenInfo) IsExpired() bool {
	if t.ExpiresAt.IsZero() {
		return false // Never expires
	}
	return time.Now().After(t.ExpiresAt)
}

type Service struct {
	registryv1alpha1connect.UnimplementedCodeGenerationServiceHandler
	conf     *config.Config
	server   *http.Server
	cert     *tls.Certificate
	mu       sync.RWMutex // protects tokens and users
	tokens   map[string]*tokenInfo
	users    map[string]string
	plugins  map[string]*codegen.Plugin
	ofs      *ocifs.OCIFS
	regCreds map[string]authn.AuthConfig
	casReg   *registry.Registry
}

func New(c *config.Config) (*Service, error) {
	// CAS storage is required
	if c.CacheDir == "" {
		return nil, fmt.Errorf("cache_dir is required for CAS storage")
	}

	svc := &Service{
		conf:     c,
		tokens:   map[string]*tokenInfo{},
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
		// Admin token never expires
		svc.tokens[svc.conf.AdminToken] = &tokenInfo{Username: "admin"}
	}

	for k, v := range svc.users {
		// Static user tokens never expire
		svc.tokens[v] = &tokenInfo{Username: k}
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

	// Initialize CAS storage using gocloud.dev
	// fileblob URL format: file:///absolute/path?create_dir=true
	blobURL := "file://" + c.CacheDir + "/cas/blobs?create_dir=true"
	if c.Storage != nil && c.Storage.BlobURL != "" {
		blobURL = c.Storage.BlobURL
	}

	bucket, err := blob.OpenBucket(context.Background(), blobURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open blob bucket: %w", err)
	}
	blobStore := storage.NewBlobStore(bucket)
	manifestStore := storage.NewManifestStore(bucket)
	slog.Info("Blob storage initialized", "url", blobURL)

	// Initialize metadata storage using docstore
	docstoreURL := "mem://"
	if c.Storage != nil && c.Storage.DocstoreURL != "" {
		docstoreURL = c.Storage.DocstoreURL
	}

	owners, modules, commits, labels, err := openDocstoreCollections(docstoreURL, c.CacheDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open docstore: %w", err)
	}
	metadataStore := storage.NewMetadataStore(owners, modules, commits, labels)
	slog.Info("Metadata storage initialized", "url", docstoreURL)

	svc.casReg = registry.New(blobStore, manifestStore, metadataStore, c.Host)
	slog.Info("CAS registry initialized")

	mux := http.NewServeMux()

	intcptrs := []connect.Interceptor{}

	otelInt, err := otelconnect.NewInterceptor()
	if err != nil {
		return nil, err
	}
	intcptrs = append(intcptrs, otelInt)

	if !c.NoLogin {
		intcptrs = append(intcptrs, newAuthInterceptor(svc))
	}

	interceptors := connect.WithInterceptors(intcptrs...)

	mux.Handle(registryv1alpha1connect.NewCodeGenerationServiceHandler(svc, interceptors))
	mux.Handle(registryv1alpha1connect.NewAuthnServiceHandler(NewAuthnService(svc), interceptors))
	mux.Handle(modulev1beta1connect.NewCommitServiceHandler(svc, interceptors))
	mux.Handle(modulev1beta1connect.NewGraphServiceHandler(svc, interceptors))
	mux.Handle(modulev1beta1connect.NewDownloadServiceHandler(svc, interceptors))
	mux.Handle(modulev1connect.NewModuleServiceHandler(NewModuleService(svc), interceptors))
	mux.Handle(modulev1connect.NewUploadServiceHandler(NewUploadService(svc), interceptors))
	mux.Handle(modulev1connect.NewGraphServiceHandler(NewGraphServiceV1(svc), interceptors))
	mux.Handle(modulev1connect.NewDownloadServiceHandler(NewDownloadServiceV1(svc), interceptors))
	mux.Handle(modulev1connect.NewCommitServiceHandler(NewCommitServiceV1(svc), interceptors))
	mux.Handle(ownerv1connect.NewOwnerServiceHandler(svc, interceptors))

	mux.Handle("/readyz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ready")
	}))
	mux.Handle("/livez", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "live")
	}))

	// OAuth2 device authorization flow for buf login (proxies to OIDC provider)
	oauth2Svc := NewOAuth2Service(svc)
	mux.Handle(DeviceRegistrationPath, oauth2Svc.Handler())
	mux.Handle(DeviceAuthorizationPath, oauth2Svc.Handler())
	mux.Handle(DeviceTokenPath, oauth2Svc.Handler())

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

func newAuthInterceptor(svc *Service) connect.UnaryInterceptorFunc {
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

			// Use mutex for thread-safe token lookup (tokens can be added dynamically via OIDC)
			svc.mu.RLock()
			info, ok := svc.tokens[tokenString]
			svc.mu.RUnlock()
			if !ok {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid token"))
			}

			// Check if token is expired
			if info.IsExpired() {
				// Remove expired token
				svc.mu.Lock()
				delete(svc.tokens, tokenString)
				svc.mu.Unlock()
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("token expired"))
			}

			// Slide expiration for dynamic tokens (those with non-zero ExpiresAt)
			if !info.ExpiresAt.IsZero() {
				svc.mu.Lock()
				info.ExpiresAt = time.Now().Add(svc.conf.GetTokenTTL())
				svc.mu.Unlock()
			}

			ctx = contextWithUser(ctx, info.Username)

			return next(ctx, req)
		}
	}
}

// debugMiddleware logs all HTTP requests for debugging.
func debugMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read body for logging (if small enough)
		var bodyPreview string
		if r.Body != nil && r.ContentLength > 0 && r.ContentLength < 4096 {
			body, err := io.ReadAll(r.Body)
			if err == nil {
				bodyPreview = string(body)
				// Restore body for handler
				r.Body = io.NopCloser(strings.NewReader(string(body)))
			}
		}

		// Build headers map for logging
		headers := make(map[string]string)
		for k, v := range r.Header {
			if len(v) == 1 {
				headers[k] = v[0]
			} else {
				headers[k] = fmt.Sprintf("%v", v)
			}
		}

		slog.Debug("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"headers", headers,
			"content-length", r.ContentLength,
			"body", bodyPreview,
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

// openDocstoreCollections opens the four docstore collections needed for metadata.
// For memdocstore (mem://), it creates collections that persist to files in cacheDir.
func openDocstoreCollections(urlBase, cacheDir string) (owners, modules, commits, labels *docstore.Collection, err error) {
	if strings.HasPrefix(urlBase, "mem://") {
		// Use memdocstore with file persistence
		metadataDir := cacheDir + "/cas/metadata"
		if err := os.MkdirAll(metadataDir, 0755); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to create metadata directory: %w", err)
		}

		owners, err = memdocstore.OpenCollection("ID", &memdocstore.Options{
			Filename: metadataDir + "/owners.json",
		})
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to open owners collection: %w", err)
		}
		modules, err = memdocstore.OpenCollection("ID", &memdocstore.Options{
			Filename: metadataDir + "/modules.json",
		})
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to open modules collection: %w", err)
		}
		commits, err = memdocstore.OpenCollection("ID", &memdocstore.Options{
			Filename: metadataDir + "/commits.json",
		})
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to open commits collection: %w", err)
		}
		labels, err = memdocstore.OpenCollection("ID", &memdocstore.Options{
			Filename: metadataDir + "/labels.json",
		})
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to open labels collection: %w", err)
		}
		return owners, modules, commits, labels, nil
	}

	// For other docstore URLs, open collections using the URL
	// The URL should be the base, and we append collection names
	owners, err = docstore.OpenCollection(context.Background(), urlBase+"/owners?id_field=ID")
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to open owners collection: %w", err)
	}
	modules, err = docstore.OpenCollection(context.Background(), urlBase+"/modules?id_field=ID")
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to open modules collection: %w", err)
	}
	commits, err = docstore.OpenCollection(context.Background(), urlBase+"/commits?id_field=ID")
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to open commits collection: %w", err)
	}
	labels, err = docstore.OpenCollection(context.Background(), urlBase+"/labels?id_field=ID")
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to open labels collection: %w", err)
	}
	return owners, modules, commits, labels, nil
}

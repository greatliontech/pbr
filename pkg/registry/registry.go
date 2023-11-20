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
	"github.com/containers/storage"
	"github.com/containers/storage/types"
	"github.com/greatliontech/pbr/pkg/config"
	"github.com/greatliontech/pbr/pkg/repository"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type Registry struct {
	conf    *config.Config
	remotes map[string]registryv1alpha1connect.ResolveServiceClient
	repos   map[string]*repository.Repository
	store   storage.Store
	server  *http.Server
	cert    *tls.Certificate
}

func New(c *config.Config, opts ...Option) (*Registry, error) {
	reg := &Registry{
		conf:    c,
		remotes: map[string]registryv1alpha1connect.ResolveServiceClient{},
	}

	// apply options
	for _, o := range opts {
		o(reg)
	}

	// init container storage
	if err := reg.initStorage(); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()

	mux.Handle(registryv1alpha1connect.NewResolveServiceHandler(reg))
	mux.Handle(registryv1alpha1connect.NewDownloadServiceHandler(reg))
	mux.Handle(registryv1alpha1connect.NewCodeGenerationServiceHandler(reg))
	mux.Handle(registryv1alpha1connect.NewRepositoryServiceHandler(reg))

	addr := ":443"
	if reg.conf.Address != "" {
		addr = reg.conf.Address
	}

	reg.server = &http.Server{
		Addr:         addr,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		Handler:      mux,
	}

	if reg.cert != nil {
		reg.server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{*reg.cert}}
	}

	for k, v := range c.Credentials.Bsr {
		reg.remotes[k] = registryv1alpha1connect.NewResolveServiceClient(
			http.DefaultClient,
			"https://"+k,
			connect.WithInterceptors(newAuthInterceptor(v)),
		)
	}
	return reg, nil
}

func (reg *Registry) Serve() error {
	if err := http2.ConfigureServer(reg.server, nil); err != nil {
		return err
	}
	return reg.server.ListenAndServeTLS("", "")
}

func (reg *Registry) ServeH2C() error {
	h2s := &http2.Server{}
	handler := h2c.NewHandler(reg.server.Handler, h2s)
	reg.server.Handler = handler
	return reg.server.ListenAndServe()
}

func (reg *Registry) initStorage() error {
	options, err := types.DefaultStoreOptionsAutoDetectUID()
	if err != nil {
		return err
	}

	store, err := storage.GetStore(options)
	if err != nil {
		return err
	}
	store.Free()

	reg.store = store
	return nil
}

func (reg *Registry) getRepository(ctx context.Context, owner, repo string) (*repository.Repository, error) {
	if reg.repos == nil {
		reg.repos = map[string]*repository.Repository{}
	}
	key := owner + "/" + repo
	if reg.repos[key] == nil {
		mod, ok := reg.conf.Modules[key]
		if !ok {
			return nil, fmt.Errorf("module not found for %s", key)
		}
		target := strings.TrimSuffix(mod.Remote, "/") + "/" + owner + "/" + repo
		if mod.Replace {
			target = mod.Remote
		}
		repo, err := repository.New("https://"+target, reg.conf.Credentials.GitToken(target), 60)
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

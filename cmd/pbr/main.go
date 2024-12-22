package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"

	"github.com/greatliontech/pbr/internal/repository"
	"github.com/greatliontech/pbr/internal/store"
	"github.com/greatliontech/pbr/internal/store/mem"
	"github.com/greatliontech/pbr/pkg/config"
	"github.com/greatliontech/pbr/pkg/registry"
)

var version = "0.0.0-dev"

func main() {
	configFile := ""

	flag.StringVar(&configFile, "config-file", "/config/config.yaml", "path to config file")

	flag.Parse()

	c, err := config.FromFile(configFile)
	if err != nil {
		slog.Error("Failed to load config", "err", err)
		os.Exit(1)
	}

	logLevel := new(slog.Level)
	*logLevel = slog.LevelError
	if c.LogLevel != "" {
		if err := logLevel.UnmarshalText([]byte(c.LogLevel)); err != nil {
			slog.Error("Failed to parse log level", "err", err)
			os.Exit(1)
		}
	}

	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})

	slog.SetDefault(slog.New(handler))

	slog.Info("Starting PBR")

	regOpts := []registry.Option{}

	if c.Address != "" {
		regOpts = append(regOpts, registry.WithAddress(c.Address))
	}
	if c.Modules != nil {
		regOpts = append(regOpts, registry.WithModules(c.Modules))
	}
	if c.Plugins != nil {
		regOpts = append(regOpts, registry.WithPlugins(c.Plugins))
	}
	if c.Credentials.Git != nil {
		credStore, err := repository.NewCredentialStore(c.Credentials.Git)
		if err != nil {
			slog.Error("Failed to create git credential store", "err", err)
			os.Exit(1)
		}
		regOpts = append(regOpts, registry.WithRepoCredStore(credStore))
	}
	if c.TLS != nil {
		cert, err := tls.LoadX509KeyPair(c.TLS.CertFile, c.TLS.KeyFile)
		if err != nil {
			log.Fatal(err)
		}
		regOpts = append(regOpts, registry.WithTLSCert(&cert))
	}
	if c.CacheDir != "" {
		regOpts = append(regOpts, registry.WithCacheDir(c.CacheDir))
	}

	s := mem.New()
	if err := configToStore(context.Background(), c, s); err != nil {
		slog.Error("Failed to convert config to store", "err", err)
		os.Exit(1)
	}
	regOpts = append(regOpts, registry.WithStore(s))

	reg, err := registry.New(c.Host, regOpts...)
	if err != nil {
		slog.Error("Failed to create registry", "err", err)
		os.Exit(1)
	}

	slog.Info("Listening on", "addr", c.Address, "host", c.Host)

	if err := reg.Serve(); err != nil {
		slog.Error("Failed to start registry", "err", err)
	}
}

func configToStore(ctx context.Context, conf *config.Config, s store.Store) error {
	for k, v := range conf.Modules {
		data := strings.Split(k, "/")
		if len(data) != 2 {
			return fmt.Errorf("invalid module key: %s", k)
		}
		owner := data[0]
		module := data[1]
		o, err := s.GetOwnerByName(ctx, owner)
		if err != nil {
			if err == store.ErrNotFound {
				if o, err = s.CreateOwner(ctx, &store.Owner{Name: owner}); err != nil {
					return fmt.Errorf("failed to create owner %s: %w", owner, err)
				}
			} else {
				return fmt.Errorf("failed to get owner %s: %w", owner, err)
			}
		}
		m, err := s.CreateModule(ctx, &store.Module{
			OwnerID: o.ID,
			Name:    module,
			RepoURL: v.Remote,
			Root:    v.Path,
			Filters: v.Filters,
			Shallow: v.Shallow,
		})
		if err != nil && err != store.ErrAlreadyExists {
			return fmt.Errorf("failed to create module %s: %w", k, err)
		}
		fmt.Println(m)
	}
	return nil
}

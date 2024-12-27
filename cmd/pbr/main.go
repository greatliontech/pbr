package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/greatliontech/pbr/internal/repository"
	"github.com/greatliontech/pbr/internal/store"
	"github.com/greatliontech/pbr/internal/store/mem"
	"github.com/greatliontech/pbr/internal/telemetry"
	"github.com/greatliontech/pbr/pkg/config"
	"github.com/greatliontech/pbr/pkg/registry"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
)

var version = "0.0.0-dev"

func main() {
	// Create a context that is canceled when SIGTERM or SIGINT is received.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

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

	slog.Info("Starting PBR", "version", version)

	// Set up telemetry
	telShutdown, err := setupTelemetry(ctx)
	if err != nil {
		slog.Error("Failed to set up telemetry", "err", err)
		os.Exit(1)
	}

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
	regOpts = append(regOpts, registry.WithAdminToken(c.AdminToken))

	s := mem.New()
	if err := configToStore(context.Background(), c, s); err != nil {
		slog.Error("Failed to convert config to store", "err", err)
		os.Exit(1)
	}
	regOpts = append(regOpts, registry.WithStore(s))
	regOpts = append(regOpts, registry.WithUsers(c.Users))

	if c.Credentials.ContainerRegistry != nil {
		regCreds := map[string]authn.AuthConfig{}
		for k, v := range c.Credentials.ContainerRegistry {
			regCreds[k] = authn.AuthConfig(v)
		}
		regOpts = append(regOpts, registry.WithRegistryCreds(regCreds))
	}

	reg, err := registry.New(c.Host, regOpts...)
	if err != nil {
		slog.Error("Failed to create registry", "err", err)
		os.Exit(1)
	}

	slog.Info("Listening on", "addr", c.Address, "host", c.Host)

	go func() {
		if err := reg.Serve(ctx); err != nil {
			slog.Error("Failed to start registry", "err", err)
			os.Exit(1)
		}
	}()

	// Wait for a termination signal
	<-ctx.Done()
	slog.Info("Shutdown signal received")

	shutdownPeriod := 30
	if sdp, ok := os.LookupEnv("TERMINATION_GRACE_PERIOD"); ok {
		sdpi, err := strconv.Atoi(sdp)
		if err == nil {
			shutdownPeriod = sdpi
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(shutdownPeriod)*time.Second)
	defer cancel()

	if err := reg.Shutdown(ctx); err != nil {
		slog.Error("Failed to shutdown registry", "err", err)
	}

	if err := telShutdown(ctx); err != nil {
		slog.Error("Failed to shutdown telemetry", "err", err)
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

func setupTelemetry(ctx context.Context) (func(context.Context) error, error) {
	instanceId := os.Getenv("SEVICE_INSTANCE_ID")
	ns := os.Getenv("SERVICE_NAMESPACE")

	otelgrpc, error := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint("localhost:4317"),
	)
	if error != nil {
		return nil, error
	}

	return telemetry.Setup(ctx,
		telemetry.WithVersion(version),
		telemetry.WithInstanceId(instanceId),
		telemetry.WithNamespace(ns),
		telemetry.WithTraceExporter(otelgrpc),
	)
}

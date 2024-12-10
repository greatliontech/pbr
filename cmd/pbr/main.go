package main

import (
	"crypto/tls"
	"flag"
	"log"
	"log/slog"
	"os"

	"github.com/gobwas/glob"
	"github.com/greatliontech/pbr/pkg/config"
	"github.com/greatliontech/pbr/pkg/registry"
	"github.com/greatliontech/pbr/pkg/repository"
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

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})

	slog.SetDefault(slog.New(handler))

	slog.Info("Starting PBR")

	regOpts := []registry.Option{}

	if c.Address != "" {
		regOpts = append(regOpts, registry.WithAddress(c.Address))
	}
	if c.Modules != nil {
		mods := []registry.Module{}
		for k, v := range c.Modules {
			g, err := glob.Compile(k)
			if err != nil {
				slog.Error("Failed to compile glob", "str", k, "err", err)
				os.Exit(1)
			}
			mods = append(mods, registry.Module{
				Match: g,
				Mod:   v,
			})
		}
		if len(mods) != 0 {
			regOpts = append(regOpts, registry.WithModules(mods))
		}
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

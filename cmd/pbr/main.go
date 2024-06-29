package main

import (
	"crypto/tls"
	"log"
	"log/slog"
	"os"

	"github.com/greatliontech/pbr/pkg/config"
	"github.com/greatliontech/pbr/pkg/registry"
	"github.com/greatliontech/pbr/pkg/repository"
)

func main() {
	slog.Info("Starting PBR")

	c, err := config.FromFile("./config.yaml")
	if err != nil {
		slog.Error("Failed to load config", "err", err)
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
	if c.Credentials.Bsr != nil {
		regOpts = append(regOpts, registry.WithBSRCreds(c.Credentials.Bsr))
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

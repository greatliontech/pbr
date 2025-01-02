package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/greatliontech/pbr/internal/config"
	"github.com/greatliontech/pbr/internal/service"
	"github.com/greatliontech/pbr/internal/telemetry"
	slogotel "github.com/remychantenay/slog-otel"
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

	slog.SetDefault(slog.New(slogotel.OtelHandler{
		Next: handler,
	}))

	slog.Info("Starting PBR", "version", version)

	// Set up telemetry
	telShutdown, err := setupTelemetry(ctx)
	if err != nil {
		slog.Error("Failed to set up telemetry", "err", err)
		os.Exit(1)
	}

	svc, err := service.New(c)
	if err != nil {
		slog.Error("Failed to create registry", "err", err)
		os.Exit(1)
	}

	slog.Info("Listening on", "addr", c.Address, "host", c.Host)

	go func() {
		if err := svc.Serve(ctx); err != nil {
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

	if err := svc.Shutdown(ctx); err != nil {
		slog.Error("Failed to shutdown registry", "err", err)
	}

	if err := telShutdown(ctx); err != nil {
		slog.Error("Failed to shutdown telemetry", "err", err)
	}
}

func setupTelemetry(ctx context.Context) (func(context.Context) error, error) {
	instanceId := os.Getenv("SEVICE_INSTANCE_ID")
	ns := os.Getenv("SERVICE_NAMESPACE")

	return telemetry.Setup(ctx,
		telemetry.WithVersion(version),
		telemetry.WithInstanceId(instanceId),
		telemetry.WithNamespace(ns),
	)
}

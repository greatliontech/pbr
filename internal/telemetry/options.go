package telemetry

import (
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
)

type options struct {
	traceExporter  trace.SpanExporter
	metricExporter metric.Exporter
	logExporter    log.Exporter
	version        string
	instanceId     string
	ns             string
}

type Option func(*options)

func WithTraceExporter(exporter trace.SpanExporter) Option {
	return func(o *options) {
		o.traceExporter = exporter
	}
}

func WithMetricExporter(exporter metric.Exporter) Option {
	return func(o *options) {
		o.metricExporter = exporter
	}
}

func WithLogExporter(exporter log.Exporter) Option {
	return func(o *options) {
		o.logExporter = exporter
	}
}

func WithVersion(version string) Option {
	return func(o *options) {
		o.version = version
	}
}

func WithInstanceId(instanceId string) Option {
	return func(o *options) {
		o.instanceId = instanceId
	}
}

func WithNamespace(ns string) Option {
	return func(o *options) {
		o.ns = ns
	}
}

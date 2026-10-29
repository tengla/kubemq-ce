package config

import "slices"

// TracingConfig exports OpenTelemetry traces over OTLP. The standard
// OTEL_EXPORTER_OTLP_* environment variables also work, for settings not
// covered here such as headers.
type TracingConfig struct {
	Enable bool `json:"enable"`
	// Endpoint is the collector's host:port. Empty uses
	// OTEL_EXPORTER_OTLP_ENDPOINT, or localhost on the protocol's port.
	Endpoint string `json:"endpoint"`
	// Protocol is grpc (port 4317) or http (port 4318).
	Protocol string `json:"protocol"`
	// Insecure sends to the collector without TLS.
	Insecure    bool   `json:"insecure"`
	ServiceName string `json:"serviceName"`
	// SampleRatio is the share of new traces recorded, from 0 to 1. Traces
	// started by a sampled client are always recorded.
	SampleRatio float64 `json:"sampleRatio"`
}

func defaultTracingConfig() *TracingConfig {
	bindViperEnv(
		"Tracing.Enable",
		"Tracing.Endpoint",
		"Tracing.Protocol",
		"Tracing.Insecure",
		"Tracing.ServiceName",
		"Tracing.SampleRatio",
	)
	return &TracingConfig{
		Enable:      false,
		Endpoint:    "",
		Protocol:    "grpc",
		Insecure:    false,
		ServiceName: "kubemq",
		SampleRatio: 1,
	}
}

func (t *TracingConfig) Validate() error {
	if !t.Enable {
		return nil
	}
	if !slices.Contains([]string{"grpc", "http"}, t.Protocol) {
		return NewConfigurationErrorf("bad tracing configuration: protocol %q must be grpc or http", t.Protocol)
	}
	if t.SampleRatio < 0 || t.SampleRatio > 1 {
		return NewConfigurationError("bad tracing configuration: sampleRatio must be between 0 and 1")
	}
	return nil
}

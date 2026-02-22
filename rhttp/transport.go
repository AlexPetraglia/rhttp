package rhttp

import "net/http"

// NewTransport builds an http.RoundTripper with rhttp middleware.
// Chain order (outer → inner): OTel → Retry → Breaker → Base.
//
// Features are enabled/disabled by config only.
func NewTransport(base http.RoundTripper, opts ...Option) http.RoundTripper {
	cfg := defaultConfig(base)
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return buildTransport(cfg)
}

// NewClient returns an *http.Client using an rhttp-composed transport.
// If no base transport is provided, http.DefaultTransport is used.
func NewClient(opts ...Option) *http.Client {
	cfg := defaultConfig(http.DefaultTransport)
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &http.Client{Transport: buildTransport(cfg)}
}

func buildTransport(cfg Config) http.RoundTripper {
	rt := cfg.Base

	// Inner → outer build (so final order is outer → inner as documented).
	if cfg.Breaker.Enabled {
		rt = &breakerTransport{next: rt, cfg: cfg.Breaker}
	}
	if cfg.Retry.Enabled {
		rt = &retryTransport{next: rt, cfg: cfg.Retry}
	}
	if cfg.OTel.Enabled {
		rt = &otelTransport{next: rt, cfg: cfg.OTel}
	}
	return rt
}

// --- Stub transports (real logic lands in v0.2+ / v0.3+ / v0.4+) ---

type retryTransport struct {
	next http.RoundTripper
	cfg  RetryConfig
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.next.RoundTrip(req)
}

type breakerTransport struct {
	next http.RoundTripper
	cfg  BreakerConfig
}

func (t *breakerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.next.RoundTrip(req)
}

type otelTransport struct {
	next http.RoundTripper
	cfg  OTelConfig
}

func (t *otelTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.next.RoundTrip(req)
}

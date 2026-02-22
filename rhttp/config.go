package rhttp

import "net/http"

// Option configures rhttp behavior.
type Option func(*Config)

// Config is the internal configuration built from defaults + options.
type Config struct {
	Base    http.RoundTripper
	Retry   RetryConfig
	Breaker BreakerConfig
	OTel    OTelConfig
}

type RetryConfig struct {
	Enabled bool
}

type BreakerConfig struct {
	Enabled bool
}

type OTelConfig struct {
	Enabled bool
}

func defaultConfig(base http.RoundTripper) Config {
	if base == nil {
		base = http.DefaultTransport
	}
	return Config{
		Base: base,
		Retry: RetryConfig{
			Enabled: false,
		},
		Breaker: BreakerConfig{
			Enabled: false,
		},
		OTel: OTelConfig{
			Enabled: false,
		},
	}
}

// WithBaseTransport sets the base transport used at the end of the chain.
// If not set, http.DefaultTransport is used.
func WithBaseTransport(rt http.RoundTripper) Option {
	return func(c *Config) {
		if rt == nil {
			rt = http.DefaultTransport
		}
		c.Base = rt
	}
}

func WithRetry(cfg RetryConfig) Option {
	return func(c *Config) { c.Retry = cfg }
}

func WithBreaker(cfg BreakerConfig) Option {
	return func(c *Config) { c.Breaker = cfg }
}

func WithOTel(cfg OTelConfig) Option {
	return func(c *Config) { c.OTel = cfg }
}

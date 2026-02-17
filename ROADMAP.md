# rhttp Roadmap

## Purpose
rhttp is a resilient HTTP transport for Go `net/http` that provides:
- Retry with safe defaults
- Circuit breaker per host (using `github.com/sony/gobreaker`)
- Observability with OpenTelemetry (metrics + tracing), backend-agnostic via OTLP

## Guiding principles
- Idiomatic `net/http` integration: implemented as `http.RoundTripper` middleware.
- Fixed default chain order (outer → inner): **OTel → Retry → Circuit Breaker → Base RoundTripper**.
- Features are enabled/disabled by configuration only (no custom ordering in v1).
- Safe-by-default retry behavior (idempotent methods, replayable bodies).
- Metrics with safe default labels (avoid high-cardinality labels like URL path).

## Versions

### v0.1.0 — Scaffold
**Goal:** establish the public shape and composition.

Deliverables:
- `NewTransport(base http.RoundTripper, opts ...Option) http.RoundTripper`
- `NewClient(opts ...Option) *http.Client`
- Config + options wiring:
  - `RetryConfig`, `BreakerConfig`, `OTelConfig`
  - `WithRetry`, `WithBreaker`, `WithOTel`, `WithBaseTransport`
- Default: features off unless enabled
- Basic tests:
  - transport composition calls base RoundTripper
  - options apply correctly

Exit criteria:
- `go test ./...` passes
- `go test ./... -race` passes
- `golangci-lint` clean (if enabled)

---

### v0.2.0 — Retry (basic, safe defaults)
**Goal:** implement retry transport with correctness first.

Default behavior:
- Retry only idempotent methods: GET, HEAD, PUT, DELETE, OPTIONS
- Retry only if request body is replayable (`Body == nil` or `GetBody != nil`)
- Retry on: network/transport errors + HTTP 5xx family
- Exponential backoff with jitter
- `MaxAttempts` (default 3)
- Always respect context cancel/deadlines

Deliverables:
- Retry transport + configuration:
  - `MaxAttempts`
  - `RetryableMethods(func(string) bool)` (default idempotent)
  - `RetryableStatus(func(int) bool)` (default 500–599)
  - `Backoff(func(attempt int, resp *http.Response, err error) time.Duration)` (default exp+jitter)
  - `ShouldRetry(func(req, resp, err) (bool, reason string))` (optional override)
- Tests:
  - retries happen for GET on 500 then succeed
  - no retry on POST by default
  - no retry when body not replayable
  - context cancellation stops retries
  - response bodies are closed between attempts (no leaks)

---

### v0.3.0 — Circuit breaker (per host)
**Goal:** integrate `sony/gobreaker` and protect upstream.

Default behavior:
- One breaker per host: key = `req.URL.Host`
- Failure counts:
  - any transport error (`err != nil`)
  - HTTP 5xx (configurable)

Deliverables:
- Breaker transport + configuration:
  - `gobreaker.Settings` exposed
  - `FailureClassifier(req, resp, err) bool` (default: err != nil OR 5xx)
  - `KeyFunc(req) string` (default: host)
- Sentinel error for open breaker (`ErrCircuitOpen`)
- Tests:
  - breaker opens after failures and fails fast
  - breaker state isolated per host
  - half-open behavior (basic probe path)

---

### v0.4.0 — OpenTelemetry (metrics + tracing)
**Goal:** provide backend-agnostic observability using OTel API.

Tracing:
- One span per overall request
- Retry attempts recorded as span events (not child spans)

Metrics:
- Safe default labels: method, host, status_class, outcome, reason
- No path/query/full URL labels by default

Deliverables:
- OTel transport + configuration:
  - Uses global providers by default
  - Optional `AddAttributes(req)` hook (must remain safe by default)
- Metrics instruments (suggested set):
  - `rhttp.client.requests_total`
  - `rhttp.client.duration_ms`
  - `rhttp.client.retries_total`
  - `rhttp.client.in_flight`
  - `rhttp.client.circuit_breaker.state`
- Example:
  - OTLP export example (Collector/Agent configured outside the library)
- Tests (light):
  - instrumentation does not panic
  - basic metric recording where feasible

---

### v0.5.0 — Retry enhancements (Retry-After + time budget)
**Goal:** add production-grade retry controls while staying safe.

Deliverables:
- `PerAttemptTimeout`
- `MaxElapsedTime`
- Optional `RespectRetryAfter`:
  - When enabled: always apply to 429
  - Optional apply to 503 (`RetryAfterOn503`)
  - Status codes remain configurable via `RetryableStatus`
- Tests:
  - Retry-After parsing (seconds and HTTP date)
  - wait calculation and caps (with injected clock)
  - max elapsed time enforcement

---

### v0.6.0 — Optional request body buffering for retries
**Goal:** allow safe retries when `GetBody` is not set, within limits.

Deliverables:
- `BodyBufferBytes` option:
  - Buffer request body up to limit and set `GetBody`
  - If body exceeds limit: fallback to strict behavior (no retry)
- Tests:
  - buffered body retries send identical payload
  - over-limit fallback behavior
  - no resource leaks

---

### v1.0.0 — Stabilization and API freeze
**Goal:** finalize APIs, docs, and examples.

Deliverables:
- API freeze + compatibility notes
- Clear documentation:
  - README quickstart
  - docs/design.md
  - docs/metrics.md
- Examples:
  - basic usage
  - breaker demo
  - OTLP demo
- CI:
  - `go test ./... -race`
  - lint
  - coverage target (optional)
- Benchmarks (optional)

## Future ideas (post-1.0)
- Optional logging hooks (slog-compatible)
- Optional request hedging (advanced)
- Rate limiting / bulkheads (advanced)
- More configurable breaker keying (host+port, host+scheme, custom)

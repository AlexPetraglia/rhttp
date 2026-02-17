# rhttp — Design Notes

## 1. Purpose
rhttp is a resilient HTTP transport for Go’s `net/http`. It adds:
- Retry (safe defaults)
- Circuit breaker (per host)
- Observability via OpenTelemetry (metrics and tracing)

rhttp is implemented as `http.RoundTripper` middleware so it composes cleanly with the standard library and other transports.

## 2. Goals
- Idiomatic `net/http` integration:
  - Works by setting `http.Client.Transport`
  - Can wrap any existing `http.RoundTripper`
- Correct retry behavior:
  - Safe defaults (idempotent methods only, no body replay bugs)
  - Backoff with jitter
  - Respects context cancellation/deadlines
- Circuit breaker per host using `github.com/sony/gobreaker`
- Backend-agnostic observability:
  - Uses OpenTelemetry API
  - Exports via OTLP through user configuration (Collector/Agent/etc.)
- Low-cardinality metrics by default (does not label by URL path)

## 3. Non-goals
- Not a service mesh, proxy, or gateway.
- Not reimplementing connection pooling, HTTP/2, DNS, etc. (`net/http` already does this).
- Not binding to Prometheus libraries directly.
- Not automatically adding logging as a hard dependency (may be added as optional hooks later).

## 4. Core architecture

### 4.1 Fixed transport chain order
rhttp uses a fixed default chain order (outer → inner):

1) OTel instrumentation  
2) Retry  
3) Circuit breaker (per host)  
4) Base `RoundTripper` (e.g., `http.DefaultTransport`)

Features are enabled/disabled by configuration only. There is no user-configurable ordering in v1.

Rationale:
- OTel outermost measures the whole operation (including retry waiting).
- Circuit breaker should fail fast when open.
- Retry should not keep attempting when the breaker is open.

### 4.2 Public API shape (planned)
- package: `rhttp`
- constructors:
  - `NewTransport(base http.RoundTripper, opts ...Option) http.RoundTripper`
  - `NewClient(opts ...Option) *http.Client` (uses `http.DefaultTransport` unless overridden)
- options configure feature configs:
  - `WithRetry(RetryConfig)`
  - `WithBreaker(BreakerConfig)`
  - `WithOTel(OTelConfig)`
  - `WithBaseTransport(rt http.RoundTripper)`

## 5. Retry design

### 5.1 Default retry safety rules
A retry is allowed only if all are true:

1) Method is retryable by default:
   - `GET`, `HEAD`, `PUT`, `DELETE`, `OPTIONS`
   - `POST` is not retried by default

2) Request body is replayable:
   - `req.Body == nil`, OR
   - `req.GetBody != nil`  
   If `req.Body` is non-nil and `req.GetBody` is nil, rhttp will not retry by default (to avoid sending an empty/partial body on later attempts).

3) Error/status is retryable:
   - Network/transport errors that look transient, and/or
   - HTTP status in configured “retryable status” function (default: 500–599)

4) The request context is not done:
   - If `ctx` is canceled or deadline exceeded, stop immediately.

### 5.2 Why body replay matters
In Go, `req.Body` is usually a one-shot stream. After the first attempt, it is consumed. Retrying the same request without a way to recreate the body can silently send the wrong payload.

Therefore the default is strict and safe:
- If body is not replayable, do not retry.

Future feature (later version):
- Optional in-memory buffering up to a configured limit, which sets `GetBody` for safe retries.
- If the body is too large, rhttp will fall back to strict behavior (no retry), not fail the request.

### 5.3 Backoff and jitter
Default backoff:
- Exponential backoff
- Jitter to avoid “thundering herd” retries from many clients at once
- Maximum delay cap (to avoid unbounded waits)

### 5.4 Limits and time budget
RetryConfig supports:
- `MaxAttempts` (default 3)
- `PerAttemptTimeout` (optional; 0 means disabled)
- `MaxElapsedTime` (optional; 0 means disabled)

Always respects:
- context cancellation/deadline

### 5.5 Retry-After support (optional)
When enabled, rhttp will respect the `Retry-After` response header:
- Always applies to HTTP 429
- Optionally applies to HTTP 503 (config flag)

Parsing:
- Retry-After seconds (e.g., `"3"`)
- Retry-After HTTP date (RFC1123 style)

Wait time calculation:
- `wait = max(backoffDelay, retryAfterDelay)`
- capped by remaining context deadline and `MaxElapsedTime` (if set)

If Retry-After is invalid/unparseable:
- ignore it and use `backoffDelay`

### 5.6 Configurability
Retry behavior is configurable via:
- `RetryableStatus(code int) bool`
- `RetryableMethods(method string) bool`
- `ShouldRetry(req, resp, err) (bool, reason string)` for full override
- `Backoff(attempt, resp, err) time.Duration` for full override

Reason strings should be stable (used for metrics):
- `net_error`
- `timeout`
- `http_5xx`
- `http_429`
- `http_503`
- `body_not_replayable`
- `ctx_done`
- `cb_open`

## 6. Circuit breaker design

### 6.1 Dependency
Uses `github.com/sony/gobreaker`.

### 6.2 Keying strategy
Circuit breaker is per host:
- key = `req.URL.Host`

A cache maps `host → *gobreaker.CircuitBreaker` (concurrency-safe).

### 6.3 Failure classification
Default: count as failure if:
- `err != nil`, OR
- `resp != nil` and `resp.StatusCode` is 500–599

Configurable via:
- `FailureClassifier(req, resp, err) bool`

### 6.4 Open breaker behavior
If breaker is open:
- request fails fast
- return a stable sentinel error (e.g., `ErrCircuitOpen`) wrapping breaker error
- retry layer treats `ErrCircuitOpen` as non-retryable by default

### 6.5 Settings
BreakerConfig exposes `gobreaker.Settings` so users can tune:
- `Interval`
- `Timeout`
- `ReadyToTrip`
- `MaxRequests` (half-open)
- `OnStateChange` hook (used to publish metrics)

## 7. OpenTelemetry design

### 7.1 Providers
By default, rhttp uses global OTel providers:
- tracer = `otel.Tracer(TracerName)`
- meter  = `otel.Meter(MeterName)`

Users configure exporters (OTLP, etc.) outside the library.

### 7.2 Tracing policy
- Exactly one span per overall request.
- Retry attempts are recorded as span events (not child spans).

Span attributes include:
- `http.request.method`
- `url.scheme`
- `server.address` (host)
- `server.port` (if available)
- resilient attributes such as:
  - `rhttp.retry.max_attempts`
  - `rhttp.attempts_total` (at end)
  - `rhttp.retries_total` (at end)

Span events per attempt:
- event name: `retry.attempt`
- attributes: `attempt_number`, `wait_ms`, `reason`, `status_code` or error type

### 7.3 Metrics policy (safe defaults)
Metrics must avoid high-cardinality labels. By default:
- Allowed labels: method, host, status_class, outcome, reason (bounded values)
- Not used: URL path, query params, full URL, user IDs

Suggested instruments:
- `rhttp.client.requests_total` (Counter)
  - labels: method, host, status_class, outcome
- `rhttp.client.duration_ms` (Histogram)
  - labels: method, host, status_class, outcome
- `rhttp.client.retries_total` (Counter)
  - labels: method, host, reason
- `rhttp.client.in_flight` (UpDownCounter)
  - labels: host (or none)
- `rhttp.client.circuit_breaker.state` (ObservableGauge)
  - labels: host, state

status_class values:
- `2xx`, `3xx`, `4xx`, `5xx`, `none`

outcome values:
- `success`, `error`, `timeout`, `canceled`, `cb_open`

## 8. Error handling rules
- Always close response bodies from failed attempts before retrying to prevent leaks.
- Never retry after `ctx.Done()`.
- Do not mutate the caller’s request unexpectedly:
  - clone the request per attempt (`req.Clone(ctx)`)
  - handle body carefully (replayable checks)
- Keep sentinel errors stable for callers and tests.

## 9. Testing strategy
Focus on correctness and leak prevention.

Unit tests:
- retries on GET 500 then success
- no retry on POST by default
- no retry when body is not replayable
- context cancellation stops retries immediately
- response bodies are closed between attempts
- breaker opens and fails fast
- breaker state isolated per host

Integration tests:
- `httptest.Server` that fails N times then succeeds
- custom `RoundTripper` that returns controlled errors

Run:
- `go test ./... -race`

## 10. Versioning and roadmap alignment
See `ROADMAP.md` for planned releases and feature milestones. The library will evolve through v0.x while APIs may change. v1.0.0 is the API freeze target.

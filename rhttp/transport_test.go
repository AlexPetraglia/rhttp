package rhttp

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

type recordingRT struct {
	calls int
	last  *http.Request
	resp  *http.Response
	err   error
}

func (rt *recordingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.calls++
	rt.last = req
	if rt.resp != nil || rt.err != nil {
		return rt.resp, rt.err
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewBufferString("ok")),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestNewTransport_Disabled_CallsBase(t *testing.T) {
	base := &recordingRT{}

	tr := NewTransport(base /* no opts */)
	req, _ := http.NewRequest("GET", "http://example.com", nil)

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()

	if base.calls != 1 {
		t.Fatalf("expected base to be called once, got %d", base.calls)
	}
}

func TestNewTransport_ChainOrder_AllEnabled(t *testing.T) {
	base := &recordingRT{}

	tr := NewTransport(base,
		WithOTel(OTelConfig{Enabled: true}),
		WithRetry(RetryConfig{Enabled: true}),
		WithBreaker(BreakerConfig{Enabled: true}),
	)

	// The chain should be:
	// otelTransport -> retryTransport -> breakerTransport -> base
	ot, ok := tr.(*otelTransport)
	if !ok {
		t.Fatalf("expected top transport to be *otelTransport, got %T", tr)
	}

	rt, ok := ot.next.(*retryTransport)
	if !ok {
		t.Fatalf("expected otel.next to be *retryTransport, got %T", ot.next)
	}

	bt, ok := rt.next.(*breakerTransport)
	if !ok {
		t.Fatalf("expected retry.next to be *breakerTransport, got %T", rt.next)
	}

	if bt.next != base {
		t.Fatalf("expected breaker.next to be base, got %T", bt.next)
	}
}

func TestNewClient_UsesComposedTransport(t *testing.T) {
	c := NewClient(WithRetry(RetryConfig{Enabled: true}))
	if c == nil {
		t.Fatal("client is nil")
	}
	if c.Transport == nil {
		t.Fatal("client transport is nil")
	}
	if _, ok := c.Transport.(*retryTransport); !ok {
		t.Fatalf("expected client transport to be *retryTransport, got %T", c.Transport)
	}
}

func TestWithBaseTransport_AppliesToClient(t *testing.T) {
	base := &recordingRT{}
	c := NewClient(
		WithBaseTransport(base),
		WithRetry(RetryConfig{Enabled: true}),
	)

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	resp, err := c.Transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()

	if base.calls != 1 {
		t.Fatalf("expected base to be called once, got %d", base.calls)
	}
}

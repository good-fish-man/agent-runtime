package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/constant"
)

func TestUnit_TraceIDFromHTTP_UsesRequestID(t *testing.T) {
	const want = "request-id-from-client"
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set(constant.HeaderRequestID, want)

	if got := traceIDFromHTTP(req); got != want {
		t.Fatalf("trace id = %q, want %q", got, want)
	}
}

func TestUnit_TraceIDFromHTTP_ParsesTraceparent(t *testing.T) {
	const want = "4bf92f3577b34da6a3ce929d0e0e4736"
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set(constant.HeaderTraceparent, "00-"+want+"-00f067aa0ba902b7-01")

	if got := traceIDFromHTTP(req); got != want {
		t.Fatalf("trace id = %q, want %q", got, want)
	}
}

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/constant"
)

func TestRequestLoggerAddsTraceAndPreservesFailureStatus(t *testing.T) {
	handler := requestLogger(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if traceIDFromHTTP(request) != "incoming-trace" {
			t.Fatalf("request context trace = %q", traceIDFromHTTP(request))
		}
		http.Error(response, "model unavailable", http.StatusServiceUnavailable)
	}))
	request := httptest.NewRequest(http.MethodPost, "/agent?stream=false", nil)
	request.Header.Set(constant.HeaderTraceID, "incoming-trace")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.Code)
	}
	if got := response.Header().Get(constant.HeaderTraceID); got != "incoming-trace" {
		t.Fatalf("trace response header = %q", got)
	}
}

func TestRequestLoggerRecoversPanic(t *testing.T) {
	handler := requestLogger(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("broken gateway")
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if got := response.Header().Get(constant.HeaderTraceID); got == "" {
		t.Fatal("generated trace response header is empty")
	}
}

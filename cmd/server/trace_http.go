package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/good-fish-man/agent-runtime/internal/constant"
	"github.com/good-fish-man/agent-runtime/log"
)

var httpTraceHeaderCandidates = []string{
	constant.HeaderTraceID,
	constant.HeaderRequestID,
	constant.HeaderCorrelationID,
	constant.HeaderTraceparent,
}

var httpTraceResponseHeaders = []string{
	constant.HeaderTraceID,
	constant.HeaderRequestID,
	constant.HeaderCorrelationID,
}

func traceIDFromHTTP(r *http.Request) string {
	for _, header := range httpTraceHeaderCandidates {
		raw := strings.TrimSpace(r.Header.Get(header))
		if raw == "" {
			continue
		}
		if strings.EqualFold(header, constant.HeaderTraceparent) {
			return traceIDFromTraceparent(raw)
		}
		return raw
	}
	return ""
}

func traceIDFromTraceparent(header string) string {
	parts := strings.Split(header, "-")
	if len(parts) < 4 {
		return ""
	}
	id := strings.TrimSpace(parts[1])
	if len(id) != 32 || id == "00000000000000000000000000000000" {
		return ""
	}
	return id
}

func contextWithHTTPTrace(ctx context.Context, traceID string) context.Context {
	if traceID == "" {
		return ctx
	}
	return log.WithReqID(ctx, traceID)
}

func writeTraceHeaders(w http.ResponseWriter, traceID string) {
	if traceID == "" {
		return
	}
	for _, header := range httpTraceResponseHeaders {
		w.Header().Set(header, traceID)
	}
}

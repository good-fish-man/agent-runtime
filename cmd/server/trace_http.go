package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/good-fish-man/agent-runtime/internal/constant"
	log "github.com/good-fish-man/logx"
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
	if r == nil {
		return ""
	}
	if value := log.ReqID(r.Context()); value != "" {
		return value
	}
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

func ensureHTTPTraceID(r *http.Request) string {
	if traceID := traceIDFromHTTP(r); traceID != "" {
		return traceID
	}
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return "art-" + hex.EncodeToString(value[:])
	}
	return "agent-runtime"
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

package main

import (
	"bytes"
	"context"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/good-fish-man/agent-runtime/log"
)

const errorResponseLogLimit = 4 * 1024

type responseStatusWriter struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
	body        bytes.Buffer
	err         error
}

func (w *responseStatusWriter) SetError(err error) { w.err = err }

func (w *responseStatusWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseStatusWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	remaining := errorResponseLogLimit - w.body.Len()
	if remaining > 0 {
		if len(data) < remaining {
			remaining = len(data)
		}
		_, _ = w.body.Write(data[:remaining])
	}
	written, err := w.ResponseWriter.Write(data)
	w.bytes += int64(written)
	return written, err
}

func (w *responseStatusWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		started := time.Now()
		traceID := ensureHTTPTraceID(request)
		ctx := log.WithReqID(request.Context(), traceID)
		request = request.WithContext(ctx)
		writeTraceHeaders(response, traceID)
		writer := &responseStatusWriter{ResponseWriter: response, status: http.StatusOK}
		log.InfowCtx(ctx, "http request started", "method", request.Method, "path", request.URL.EscapedPath(), "query", request.URL.RawQuery)
		defer func() {
			if recovered := recover(); recovered != nil {
				log.ErrorfCtx(ctx, "http panic method=%s path=%s err=%v\n%s", request.Method, request.URL.RequestURI(), recovered, debug.Stack())
				if !writer.wroteHeader {
					http.Error(writer, "internal server error", http.StatusInternalServerError)
				}
			}
			logHTTPCompletion(ctx, request, writer, time.Since(started))
		}()
		next.ServeHTTP(writer, request)
	})
}

func logHTTPCompletion(ctx context.Context, request *http.Request, writer *responseStatusWriter, elapsed time.Duration) {
	kv := []any{"method", request.Method, "path", request.URL.EscapedPath(), "query", request.URL.RawQuery, "status", writer.status, "bytes", writer.bytes, "cost_ms", elapsed.Milliseconds()}
	if writer.status < http.StatusBadRequest {
		log.InfowCtx(ctx, "http request completed", kv...)
		return
	}
	kv = append(kv, "response", string(bytes.TrimSpace(writer.body.Bytes())))
	if writer.err != nil {
		kv = append(kv, "error_chain", log.FormatError(writer.err))
	}
	if writer.status >= http.StatusInternalServerError {
		log.ErrorwCtx(ctx, "http request failed", kv...)
		return
	}
	log.WarnwCtx(ctx, "http request rejected", kv...)
}

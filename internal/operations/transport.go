package operations

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (g *Gate) WrapHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, release, err := g.Acquire(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writer := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() { release(writer.status >= http.StatusInternalServerError) }()
		next.ServeHTTP(writer, r.WithContext(ctx))
	})
}

func (g *Gate) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !admittedMethod(info.FullMethod) {
			return handler(ctx, req)
		}
		runCtx, release, err := g.Acquire(ctx)
		if err != nil {
			return nil, admissionStatus(err)
		}
		response, runErr := handler(runCtx, req)
		release(runErr != nil)
		return response, runErr
	}
}

func (g *Gate) StreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !admittedMethod(info.FullMethod) {
			return handler(srv, stream)
		}
		runCtx, release, err := g.Acquire(stream.Context())
		if err != nil {
			return admissionStatus(err)
		}
		runErr := handler(srv, &contextServerStream{ServerStream: stream, ctx: runCtx})
		release(runErr != nil)
		return runErr
	}
}

func admittedMethod(method string) bool {
	return !strings.HasSuffix(method, "/HealthCheck") &&
		!strings.HasSuffix(method, "/ListCapabilities") &&
		!strings.HasSuffix(method, "/Stop")
}

func admissionStatus(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, "runtime admission wait expired")
	}
	return status.Error(codes.ResourceExhausted, err.Error())
}

type contextServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *contextServerStream) Context() context.Context { return s.ctx }

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

package server

import (
	"context"
	"strings"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/constant"
	log "github.com/good-fish-man/logx"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestUnit_UnaryTraceInterceptor_UsesIncomingMetadata(t *testing.T) {
	const want = "trace-from-client"
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(constant.MetaKeyTraceID, want))

	interceptor := UnaryTraceInterceptor()
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Run"}, func(ctx context.Context, req any) (any, error) {
		if got := log.ReqID(ctx); got != want {
			t.Fatalf("ctx trace id = %q, want %q", got, want)
		}
		if got := log.ReqID(ctx); got != want {
			t.Fatalf("logger context trace id = %q, want %q", got, want)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
}

func TestUnit_UnaryTraceInterceptor_UsesRequestTraceAndRecoversPanic(t *testing.T) {
	const want = "trace-from-request"
	interceptor := UnaryTraceInterceptor()
	_, err := interceptor(context.Background(), tracedRequest{traceID: want}, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Run"}, func(ctx context.Context, req any) (any, error) {
		if got := log.ReqID(ctx); got != want {
			t.Fatalf("ctx trace id = %q, want %q", got, want)
		}
		panic("broken handler")
	})
	if status.Code(err) != codes.Internal || !strings.Contains(err.Error(), "broken handler") {
		t.Fatalf("panic error = %v", err)
	}
}

func TestUnit_StreamTraceInterceptor_UsesIncomingMetadata(t *testing.T) {
	const want = "stream-trace-from-client"
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(constant.MetaKeyTraceID, want))

	interceptor := StreamTraceInterceptor()
	err := interceptor(nil, fakeServerStream{ctx: ctx}, &grpc.StreamServerInfo{FullMethod: "/test.Service/RunStream"}, func(srv any, stream grpc.ServerStream) error {
		if got := log.ReqID(stream.Context()); got != want {
			t.Fatalf("stream ctx trace id = %q, want %q", got, want)
		}
		if got := log.ReqID(stream.Context()); got != want {
			t.Fatalf("logger context trace id = %q, want %q", got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
}

func TestUnit_UnaryTraceInterceptor_PreservesSourceFramesAcrossGRPC(t *testing.T) {
	interceptor := UnaryTraceInterceptor()
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Run"}, func(context.Context, any) (any, error) {
		cause := status.Error(codes.Unavailable, "provider unavailable")
		return nil, log.GRPCError(cause, codes.Unavailable, "test.Service.Run.provider", "model failed")
	})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("status code = %s, want %s", status.Code(err), codes.Unavailable)
	}
	if !strings.Contains(err.Error(), "test.Service.Run.provider") || !strings.Contains(err.Error(), "trace_interceptor_test.go:") {
		t.Fatalf("transport error is missing source frames:\n%s", err)
	}
}

type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

type tracedRequest struct{ traceID string }

func (r tracedRequest) GetTraceId() string { return r.traceID }

func (s fakeServerStream) Context() context.Context {
	return s.ctx
}

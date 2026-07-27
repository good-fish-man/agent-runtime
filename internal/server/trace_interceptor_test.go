package server

import (
	"context"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/constant"
	"github.com/good-fish-man/agent-runtime/log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestUnit_UnaryTraceInterceptor_UsesIncomingMetadata(t *testing.T) {
	const want = "trace-from-client"
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(constant.MetaKeyTraceID, want))

	interceptor := UnaryTraceInterceptor()
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Run"}, func(ctx context.Context, req any) (any, error) {
		if got, _ := ctx.Value(log.ReqIDKey).(string); got != want {
			t.Fatalf("ctx trace id = %q, want %q", got, want)
		}
		if got := log.GetReqId(); got != want {
			t.Fatalf("bound logger trace id = %q, want %q", got, want)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
}

func TestUnit_StreamTraceInterceptor_UsesIncomingMetadata(t *testing.T) {
	const want = "stream-trace-from-client"
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(constant.MetaKeyTraceID, want))

	interceptor := StreamTraceInterceptor()
	err := interceptor(nil, fakeServerStream{ctx: ctx}, &grpc.StreamServerInfo{FullMethod: "/test.Service/RunStream"}, func(srv any, stream grpc.ServerStream) error {
		if got, _ := stream.Context().Value(log.ReqIDKey).(string); got != want {
			t.Fatalf("stream ctx trace id = %q, want %q", got, want)
		}
		if got := log.GetReqId(); got != want {
			t.Fatalf("bound logger trace id = %q, want %q", got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
}

type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s fakeServerStream) Context() context.Context {
	return s.ctx
}

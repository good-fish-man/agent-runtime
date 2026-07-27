package server

import (
	"context"

	"github.com/good-fish-man/agent-runtime/log"

	"google.golang.org/grpc"
)

// UnaryTraceInterceptor binds inbound gRPC trace metadata to the request
// context before service handlers run.
func UnaryTraceInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		traceID := resolveTraceID(ctx, "")
		ctx = log.WithReqID(ctx, traceID)
		release := log.BindCtx(ctx)
		defer release()
		return handler(ctx, req)
	}
}

// StreamTraceInterceptor binds inbound gRPC trace metadata to server-streaming
// contexts before service handlers run.
func StreamTraceInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := stream.Context()
		traceID := resolveTraceID(ctx, "")
		ctx = log.WithReqID(ctx, traceID)
		release := log.BindCtx(ctx)
		defer release()
		return handler(srv, traceServerStream{ServerStream: stream, ctx: ctx})
	}
}

type traceServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s traceServerStream) Context() context.Context {
	return s.ctx
}

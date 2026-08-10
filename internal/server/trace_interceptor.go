package server

import (
	"context"
	"runtime/debug"
	"time"

	log "github.com/good-fish-man/logx"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryTraceInterceptor binds inbound gRPC trace metadata to the request
// context before service handlers run.
func UnaryTraceInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (response any, err error) {
		traceID := resolveTraceID(ctx, requestTraceID(req))
		ctx = log.WithReqID(ctx, traceID)
		release := log.BindCtx(ctx)
		defer release()
		started := time.Now()
		log.InfowCtx(ctx, "grpc request started", "method", info.FullMethod)
		defer func() {
			if recovered := recover(); recovered != nil {
				err = log.GRPCError(log.NewError("server.UnaryTraceInterceptor.panic", "panic: %v", recovered), codes.Internal, "server.UnaryTraceInterceptor", "")
				log.ErrorfCtx(ctx, "grpc panic method=%s err=%v\n%s", info.FullMethod, recovered, debug.Stack())
			}
			logGRPCCompletion(ctx, info.FullMethod, time.Since(started), err)
			err = errorForGRPCTransport(err)
		}()
		response, err = handler(ctx, req)
		return response, err
	}
}

// StreamTraceInterceptor binds inbound gRPC trace metadata to server-streaming
// contexts before service handlers run.
func StreamTraceInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		ctx := stream.Context()
		traceID := resolveTraceID(ctx, "")
		ctx = log.WithReqID(ctx, traceID)
		release := log.BindCtx(ctx)
		defer release()
		started := time.Now()
		log.InfowCtx(ctx, "grpc stream started", "method", info.FullMethod)
		defer func() {
			if recovered := recover(); recovered != nil {
				err = log.GRPCError(log.NewError("server.StreamTraceInterceptor.panic", "panic: %v", recovered), codes.Internal, "server.StreamTraceInterceptor", "")
				log.ErrorfCtx(ctx, "grpc stream panic method=%s err=%v\n%s", info.FullMethod, recovered, debug.Stack())
			}
			logGRPCCompletion(ctx, info.FullMethod, time.Since(started), err)
			err = errorForGRPCTransport(err)
		}()
		err = handler(srv, traceServerStream{ServerStream: stream, ctx: ctx})
		return err
	}
}

// errorForGRPCTransport keeps captured runtime source frames in the error sent
// to Runtime Client while retaining the original gRPC status code. Plain
// status errors remain unchanged.
func errorForGRPCTransport(err error) error {
	if err == nil {
		return nil
	}
	detail := log.FormatError(err)
	if detail == err.Error() {
		return err
	}
	return status.Error(status.Code(err), detail)
}

func requestTraceID(req any) string {
	if traced, ok := req.(interface{ GetTraceId() string }); ok {
		return traced.GetTraceId()
	}
	return ""
}

func logGRPCCompletion(ctx context.Context, method string, elapsed time.Duration, err error) {
	code := status.Code(err)
	kv := []any{"method", method, "code", code.String(), "cost_ms", elapsed.Milliseconds()}
	if err == nil {
		log.InfowCtx(ctx, "grpc request completed", kv...)
		return
	}
	kv = append(kv, "error_chain", log.FormatError(err))
	if code == codes.InvalidArgument || code == codes.NotFound || code == codes.Unauthenticated || code == codes.PermissionDenied {
		log.WarnwCtx(ctx, "grpc request rejected", kv...)
		return
	}
	log.ErrorwCtx(ctx, "grpc request failed", kv...)
}

type traceServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s traceServerStream) Context() context.Context {
	return s.ctx
}

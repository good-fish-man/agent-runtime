package log

import (
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCError adds source context while retaining the gRPC status code.
func GRPCError(err error, code codes.Code, operation, message string) error {
	if err == nil {
		return nil
	}
	if message != "" {
		err = fmt.Errorf("%s: %w", message, err)
	}
	return &grpcTracedError{Err: wrapErrorAt(err, operation, 1), Code: code}
}

type grpcTracedError struct {
	Err  error
	Code codes.Code
}

func (e *grpcTracedError) Error() string              { return e.Err.Error() }
func (e *grpcTracedError) Unwrap() error              { return e.Err }
func (e *grpcTracedError) GRPCStatus() *status.Status { return status.New(e.Code, e.Error()) }

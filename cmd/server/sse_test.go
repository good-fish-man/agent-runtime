package main

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"
)

func TestServeSSEDoesNotDuplicateStructuredError(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/run/stream", nil)
	serveSSE(recorder, request, func(send func(*runtimev1.StreamEvent) error) error {
		if err := send(&runtimev1.StreamEvent{Payload: &runtimev1.StreamEvent_Error{Error: &runtimev1.ErrorEvent{Message: "provider failed"}}}); err != nil {
			return err
		}
		return errors.New("wrapped provider failed")
	})
	if count := strings.Count(recorder.Body.String(), "event: error"); count != 1 {
		t.Fatalf("error event count = %d, body = %q", count, recorder.Body.String())
	}
}

package operations

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGateRejectsWhenQueueIsFull(t *testing.T) {
	gate := NewGate(Config{MaxInflight: 1, MaxQueue: 0, AdmissionWait: time.Second}, "instance-test")
	_, release, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release(false)
	if _, _, err := gate.Acquire(context.Background()); !errors.Is(err, ErrOverloaded) {
		t.Fatalf("expected overload, got %v", err)
	}
	if gate.SLO().DroppedEvents != 1 {
		t.Fatal("rejected admission was not counted")
	}
}

func TestGateAppliesDeadlineAndDraining(t *testing.T) {
	gate := NewGate(Config{MaxInflight: 1, RequestTimeout: 10 * time.Millisecond}, "instance-test")
	ctx, release, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	<-ctx.Done()
	release(true)
	if gate.SLO().Errors != 1 {
		t.Fatal("failed request was not counted")
	}
	gate.Drain()
	if _, _, err := gate.Acquire(context.Background()); !errors.Is(err, ErrDraining) {
		t.Fatalf("expected draining error, got %v", err)
	}
}

func TestHTTPAdmissionReportsServerFailure(t *testing.T) {
	gate := NewGate(Config{MaxInflight: 1}, "instance-test")
	handler := gate.WrapHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/run", nil))
	if recorder.Code != http.StatusInternalServerError || gate.SLO().Errors != 1 {
		t.Fatalf("unexpected HTTP metrics status=%d slo=%+v", recorder.Code, gate.SLO())
	}
}

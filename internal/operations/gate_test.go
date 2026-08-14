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
	if gate.SLO().RejectedRequests != 1 || gate.SLO().DroppedEvents != 0 {
		t.Fatal("rejected admission was not counted")
	}
}

func TestGateRejectsCancelledContextWithoutAdmission(t *testing.T) {
	gate := NewGate(Config{MaxInflight: 1}, "instance-test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := gate.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if snapshot := gate.SLO(); snapshot.Requests != 1 || snapshot.Errors != 1 || snapshot.RejectedRequests != 1 {
		t.Fatalf("cancelled admission metrics = %+v", snapshot)
	}
}

func TestDrainWakesQueuedAdmissions(t *testing.T) {
	gate := NewGate(Config{MaxInflight: 1, MaxQueue: 1, AdmissionWait: time.Minute}, "instance-test")
	_, release, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release(false)
	result := make(chan error, 1)
	go func() {
		_, _, err := gate.Acquire(context.Background())
		result <- err
	}()
	deadline := time.Now().Add(time.Second)
	for gate.queued.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	gate.Drain()
	select {
	case err := <-result:
		if !errors.Is(err, ErrDraining) {
			t.Fatalf("expected draining error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued admission was not woken by drain")
	}
}

func TestGateSeparatesTimeoutsFromDroppedEvents(t *testing.T) {
	gate := NewGate(Config{MaxInflight: 1, RequestTimeout: 5 * time.Millisecond}, "instance-test")
	ctx, release, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	<-ctx.Done()
	release(false)
	gate.RecordDroppedEvents(2)
	snapshot := gate.SLO()
	if snapshot.TimedOutRequests != 1 || snapshot.Errors != 1 || snapshot.DroppedEvents != 2 {
		t.Fatalf("unexpected SLO counters: %+v", snapshot)
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

func TestStatusWriterKeepsFirstResponseStatus(t *testing.T) {
	gate := NewGate(Config{MaxInflight: 1}, "instance-test")
	handler := gate.WrapHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusBadGateway || gate.SLO().Errors != 1 {
		t.Fatalf("first response status was not authoritative: status=%d slo=%+v", recorder.Code, gate.SLO())
	}
}

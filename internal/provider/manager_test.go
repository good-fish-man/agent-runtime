package provider

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	pluginv1 "github.com/good-fish-man/athena-protocol/protocol/plugin/v1"
)

func TestProviderFailureCircuitIsolatesRepeatedRuntimeFailure(t *testing.T) {
	manifest := testManifest()
	manifest.Runtime.StaticResponses[testCapabilityID] = json.RawMessage(`{}`)
	entry := testEntry(manifest, strings.Repeat("a", 64))
	sink := &collectingAuditSink{}
	manager := NewManager(sink)
	value := manager.add(manifest, entry, entry.ManifestSHA256)
	for attempt := 0; attempt < circuitFailureThreshold; attempt++ {
		if _, err := manager.invoke(context.Background(), value, manifest.Capabilities[0], `{"name":"Athena"}`); err == nil {
			t.Fatal("failing Provider unexpectedly succeeded")
		}
	}
	if _, err := manager.invoke(context.Background(), value, manifest.Capabilities[0], `{"name":"Athena"}`); err == nil || !strings.Contains(err.Error(), "circuit is open") {
		t.Fatalf("repeated Provider failure did not open the circuit: %v", err)
	}
	traces := sink.values()
	if len(traces) != circuitFailureThreshold+1 || traces[len(traces)-1].Status != pluginv1.InvocationUnavailable {
		t.Fatalf("circuit rejection was not audited as UNAVAILABLE: %+v", traces)
	}
}

type collectingAuditSink struct {
	mu     sync.Mutex
	traces []pluginv1.InvocationTrace
}

func (s *collectingAuditSink) Record(_ context.Context, trace pluginv1.InvocationTrace) error {
	s.mu.Lock()
	s.traces = append(s.traces, trace)
	s.mu.Unlock()
	return nil
}

func (s *collectingAuditSink) values() []pluginv1.InvocationTrace {
	s.mu.Lock()
	defer s.mu.Unlock()
	encoded, _ := json.Marshal(s.traces)
	var result []pluginv1.InvocationTrace
	_ = json.Unmarshal(encoded, &result)
	return result
}

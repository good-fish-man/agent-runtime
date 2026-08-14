package readiness

import (
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/operations"
	ga "github.com/good-fish-man/athena-protocol/protocol/ga/v1"
)

func TestBuildPassesWithProductionInvariants(t *testing.T) {
	report := Build(operations.NewGate(operations.Config{}, "runtime-1"), Config{
		Version: "1.0.0", InstanceID: "runtime-1", PluginsEnabled: true,
		PluginRequireSignature: true, PluginTrustStorePath: "/trust.json",
		MemoryEnabled: true, DatabaseEnabled: true,
	})
	if report.Status != ga.StatusPass {
		t.Fatalf("status = %s, want PASS: %+v", report.Status, report.Checks)
	}
	if err := Validate(report); err != nil {
		t.Fatal(err)
	}
}

func TestBuildFailsUnsafePluginAndMemoryConfiguration(t *testing.T) {
	report := Build(operations.NewGate(operations.Config{}, "runtime-1"), Config{
		Version: "1.0.0", InstanceID: "runtime-1", PluginsEnabled: true,
		MemoryEnabled: true,
	})
	if report.Status != ga.StatusFail {
		t.Fatalf("status = %s, want FAIL", report.Status)
	}
}

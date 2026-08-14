package provider

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/good-fish-man/agent-runtime/internal/capability"
	pluginv1 "github.com/good-fish-man/athena-protocol/protocol/plugin/v1"
)

const testCapabilityID = "com.example.echo.echo"

func TestLoadSignedProviderAndRecordInvocation(t *testing.T) {
	root := t.TempDir()
	manifest := testManifest()
	entry, cfg := writeSignedPackage(t, root, manifest)
	writeRegistry(t, cfg.RegistryPath, entry)

	registry := capability.NewRegistry()
	manager, report, err := LoadAndRegister(context.Background(), registry, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Loaded) != 1 || len(report.Rejected) != 0 || len(manager.Providers()) != 1 {
		t.Fatalf("unexpected load report: %+v", report)
	}
	resolved, unavailable, err := registry.Resolve("", []string{testCapabilityID})
	if err != nil || len(unavailable) != 0 || len(resolved) != 1 {
		t.Fatalf("resolve provider: tools=%d unavailable=%v err=%v", len(resolved), unavailable, err)
	}
	invokable, ok := resolved[0].(tool.InvokableTool)
	if !ok {
		t.Fatal("resolved provider is not invokable")
	}
	output, err := invokable.InvokableRun(context.Background(), `{"name":"Athena"}`)
	if err != nil {
		t.Fatal(err)
	}
	var observation map[string]any
	if err := json.Unmarshal([]byte(output), &observation); err != nil {
		t.Fatal(err)
	}
	if observation["schema"] != providerObservationSchema || observation["provider_id"] != manifest.ProviderID {
		t.Fatalf("unexpected observation: %s", output)
	}
	audit, err := os.ReadFile(cfg.AuditPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(audit), `"status":"SUCCEEDED"`) || !strings.Contains(string(audit), manifest.ProviderID) {
		t.Fatalf("missing invocation provenance: %s", audit)
	}
}

func TestTamperedManifestIsRejected(t *testing.T) {
	root := t.TempDir()
	manifest := testManifest()
	entry, cfg := writeSignedPackage(t, root, manifest)
	manifest.Description = "tampered"
	writeJSON(t, filepath.Join(cfg.Directory, manifest.ProviderID, manifest.Version, pluginv1.ManifestFile), manifest)
	writeRegistry(t, cfg.RegistryPath, entry)

	registry := capability.NewRegistry()
	_, report, err := LoadAndRegister(context.Background(), registry, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rejected) != 1 || len(registry.List()) != 0 {
		t.Fatalf("tampered package was not rejected: %+v", report)
	}
}

func TestRegistryCannotExpandManifestPermissions(t *testing.T) {
	root := t.TempDir()
	manifest := testManifest()
	manifest.Permissions.FilesystemRead = []string{filepath.Clean(root)}
	entry, cfg := writeSignedPackage(t, root, manifest)
	entry.GrantedPermissions.FilesystemRead = append(entry.GrantedPermissions.FilesystemRead, filepath.Join(root, ".."))
	writeRegistry(t, cfg.RegistryPath, entry)

	registry := capability.NewRegistry()
	_, report, err := LoadAndRegister(context.Background(), registry, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rejected) != 1 || len(registry.List()) != 0 {
		t.Fatalf("expanded registry permission was not rejected: %+v", report)
	}
}

func TestPartialProviderRegistrationRollsBack(t *testing.T) {
	root := t.TempDir()
	manifest := testManifest()
	collisionID := manifest.ProviderID + ".collision"
	manifest.Capabilities = append(manifest.Capabilities, pluginv1.Capability{
		ID: collisionID, Description: "Colliding capability", InputSchema: objectSchema(),
		OutputSchema: objectSchema(), ReadOnly: true, Risk: pluginv1.RiskR0,
		ObservationContract: "echo.observation.v1",
	})
	manifest.Runtime.StaticResponses[collisionID] = json.RawMessage(`{"message":"collision"}`)
	entry, cfg := writeSignedPackage(t, root, manifest)
	writeRegistry(t, cfg.RegistryPath, entry)

	registry := capability.NewRegistry()
	if err := registry.Register(capability.Definition{ID: collisionID}, nil); err != nil {
		t.Fatal(err)
	}
	manager, report, err := LoadAndRegister(context.Background(), registry, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := registry.Get(testCapabilityID); exists {
		t.Fatal("first capability remained registered after package rollback")
	}
	if _, exists := registry.Get(collisionID); !exists {
		t.Fatal("pre-existing capability was removed during package rollback")
	}
	if len(manager.Providers()) != 0 || len(report.Rejected) != 1 {
		t.Fatalf("partial provider was not rolled back: %+v", report)
	}
}

func TestAuditSinkPanicCannotCrashRuntime(t *testing.T) {
	manifest := testManifest()
	entry := testEntry(manifest, "digest")
	manager := NewManager(panicAuditSink{})
	binding := manager.add(manifest, entry, "digest")
	_, err := newProviderTool(binding, manifest.Capabilities[0]).InvokableRun(context.Background(), `{"name":"Athena"}`)
	if err == nil || !strings.Contains(err.Error(), "audit sink panic") {
		t.Fatalf("expected isolated audit error, got %v", err)
	}
}

func TestStrictJSONRejectsTrailingValues(t *testing.T) {
	var value map[string]any
	if err := decodeStrict([]byte(`{} {}`), &value); err == nil {
		t.Fatal("trailing JSON value was accepted")
	}
}

type panicAuditSink struct{}

func (panicAuditSink) Record(context.Context, pluginv1.InvocationTrace) error {
	panic("broken sink")
}

func testManifest() pluginv1.ProviderManifest {
	return pluginv1.ProviderManifest{
		Schema: pluginv1.Schema, ProviderID: "com.example.echo", Name: "Echo Fixture", Version: "0.8.0",
		Description: "Read-only signed fixture", MinRuntimeVersion: "0.8.0",
		Capabilities: []pluginv1.Capability{{
			ID: testCapabilityID, Description: "Return a fixture", InputSchema: map[string]any{
				"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}},
				"required": []any{"name"}, "additionalProperties": false,
			}, OutputSchema: objectSchema(), ReadOnly: true, Risk: pluginv1.RiskR0,
			ObservationContract: "echo.observation.v1",
		}},
		Platforms: []pluginv1.Platform{{OS: "any", Arch: "any"}}, RiskFloor: pluginv1.RiskR0,
		Resources:   pluginv1.ResourceLimits{MaxExecutionMS: 1000, MaxInputBytes: 4096, MaxOutputBytes: 4096, MaxConcurrency: 1, MaxMemoryMB: 32, MaxCPUMillis: 250},
		HealthCheck: pluginv1.HealthCheck{Operation: testCapabilityID, TimeoutMS: 500},
		Observation: pluginv1.ObservationContract{Schema: objectSchema()},
		Runtime:     pluginv1.RuntimeSpec{Kind: pluginv1.RuntimeStaticJSON, StaticResponses: map[string]json.RawMessage{testCapabilityID: json.RawMessage(`{"message":"hello"}`)}},
		SBOMRef:     pluginv1.SBOMFile, IssuedAt: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC),
	}
}

func objectSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}}, "required": []any{"message"}}
}

func writeSignedPackage(t *testing.T, root string, manifest pluginv1.ProviderManifest) (pluginv1.RegistryEntry, Config) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "packages")
	packageDir := filepath.Join(directory, manifest.ProviderID, manifest.Version)
	if err := os.MkdirAll(packageDir, 0700); err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	sbom := []byte(`{"bomFormat":"CycloneDX","specVersion":"1.5"}`)
	signature := ed25519.Sign(privateKey, append(append(append([]byte(nil), manifestBytes...), '\n'), sbom...))
	envelope := pluginv1.SignatureEnvelope{Algorithm: pluginv1.SignatureEd25519, KeyID: "test-key", Signature: base64.StdEncoding.EncodeToString(signature)}
	if err := os.WriteFile(filepath.Join(packageDir, pluginv1.ManifestFile), manifestBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, pluginv1.SBOMFile), sbom, 0600); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(packageDir, pluginv1.SignatureFile), envelope)
	trustPath := filepath.Join(root, "trust-store.json")
	writeJSON(t, trustPath, trustStore{Schema: "athena.plugin-trust.v1", Keys: []trustKey{{KeyID: "test-key", Algorithm: pluginv1.SignatureEd25519, PublicKey: base64.StdEncoding.EncodeToString(publicKey)}}})
	entry := testEntry(manifest, pluginv1.ManifestSHA256(manifestBytes))
	return entry, Config{Enabled: true, Directory: directory, RegistryPath: filepath.Join(root, "registry.json"), TrustStorePath: trustPath, AuditPath: filepath.Join(root, "audit.jsonl"), RuntimeVersion: "0.8.0", RequireSignature: true}
}

func testEntry(manifest pluginv1.ProviderManifest, digest string) pluginv1.RegistryEntry {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	return pluginv1.RegistryEntry{
		Schema: pluginv1.Schema, ProviderID: manifest.ProviderID, Version: manifest.Version,
		Status: pluginv1.StatusActive, ManifestSHA256: digest, GrantedPermissions: manifest.Permissions,
		GrantedResources: manifest.Resources, ApprovedBy: "admin", InstalledAt: now, UpdatedAt: now, Revision: 1,
	}
}

func writeRegistry(t *testing.T, path string, entries ...pluginv1.RegistryEntry) {
	t.Helper()
	writeJSON(t, path, pluginv1.RegistryIndex{Schema: pluginv1.Schema, Entries: entries})
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

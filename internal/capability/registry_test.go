package capability

import (
	"context"
	"testing"
)

func TestCatalogContainsStablePublicCapabilities(t *testing.T) {
	for _, id := range []string{InternetSearch, InternetFetch, GitHubSearch, WeatherCurrent, MapsRoute, FilesystemRead, PythonExecute} {
		if definition, ok := GlobalRegistry.Get(id); !ok || definition.ID != id {
			t.Fatalf("capability %s is not registered: %+v", id, definition)
		}
	}
}

func TestResolveExposesModelSafeCapabilityName(t *testing.T) {
	resolved, unavailable, err := GlobalRegistry.Resolve(".", []string{InternetSearch})
	if err != nil || len(unavailable) != 0 || len(resolved) != 1 {
		t.Fatalf("resolved=%d unavailable=%v err=%v", len(resolved), unavailable, err)
	}
	info, err := resolved[0].Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "internet_search" {
		t.Fatalf("model name = %q, want internet_search", info.Name)
	}
	if info.Desc == "" || info.Name == InternetSearch {
		t.Fatalf("invalid capability schema: %+v", info)
	}
}

func TestUnavailableCapabilityIsDiscoverableButNotResolved(t *testing.T) {
	definition, ok := GlobalRegistry.Get(PythonExecute)
	if !ok || definition.Status != StatusUnavailable || definition.Reason == "" {
		t.Fatalf("unexpected python capability: %+v", definition)
	}
	resolved, unavailable, err := GlobalRegistry.Resolve(".", []string{PythonExecute})
	if err != nil || len(resolved) != 0 || len(unavailable) != 1 || unavailable[0] != PythonExecute {
		t.Fatalf("resolved=%v unavailable=%v err=%v", resolved, unavailable, err)
	}
}

func TestRegistryRejectsDuplicateIDs(t *testing.T) {
	registry := NewRegistry()
	definition := Definition{ID: "test.read"}
	if err := registry.Register(definition, nil); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(definition, nil); err == nil {
		t.Fatal("duplicate capability registration was accepted")
	}
}

package capability

import (
	"context"
	"testing"
)

func TestCatalogContainsStablePublicCapabilities(t *testing.T) {
	for _, id := range []string{InternetSearch, InternetFetch, GitHubSearch, WeatherCurrent, MapsRoute, FilesystemRead, BrowserTask, PythonExecute} {
		if definition, ok := GlobalRegistry.Get(id); !ok || definition.ID != id {
			t.Fatalf("capability %s is not registered: %+v", id, definition)
		}
		definition, _ := GlobalRegistry.Get(id)
		if len(definition.Preconditions) == 0 || len(definition.Postconditions) == 0 || (!definition.ReadOnly && len(definition.ExpectedEffects) == 0) {
			t.Fatalf("capability %s has no structured world contract: %+v", id, definition)
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

func TestExternalCapabilitiesCanBeReloadedWithoutTouchingBuiltins(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Definition{ID: "builtin.read", Provider: "builtin"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterExternal(Definition{ID: "com.example.echo.read", Provider: "com.example.echo", ProviderVersion: "0.8.0"}, nil); err != nil {
		t.Fatal(err)
	}
	registry.RemoveExternal()
	if _, ok := registry.Get("com.example.echo.read"); ok {
		t.Fatal("external capability survived reload reset")
	}
	if _, ok := registry.Get("builtin.read"); !ok {
		t.Fatal("built-in capability was removed")
	}
}

func TestIsClientBound(t *testing.T) {
	clientBound := []string{BrowserTask, BrowserOpen, BrowserSearch, BrowserClose, DesktopAction}
	for _, id := range clientBound {
		if !IsClientBound(id) {
			t.Fatalf("capability %s should be client-bound", id)
		}
	}
	serverBound := []string{InternetSearch, InternetFetch, FilesystemRead, InteractionAsk, ImageGenerate, VideoGenerate}
	for _, id := range serverBound {
		if IsClientBound(id) {
			t.Fatalf("capability %s should not be client-bound", id)
		}
	}
}

func TestClientBoundModelNames(t *testing.T) {
	names := GlobalRegistry.ClientBoundModelNames()
	index := make(map[string]bool, len(names))
	for _, name := range names {
		index[name] = true
	}
	for _, want := range []string{ModelName(BrowserTask), ModelName(DesktopAction)} {
		if !index[want] {
			t.Fatalf("client-bound model names missing %q: %v", want, names)
		}
	}
	for _, absent := range []string{ModelName(InternetSearch), ModelName(InteractionAsk), ModelName(ImageGenerate)} {
		if index[absent] {
			t.Fatalf("client-bound model names unexpectedly include %q: %v", absent, names)
		}
	}
}

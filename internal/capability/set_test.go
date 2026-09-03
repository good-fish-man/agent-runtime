package capability

import (
	"reflect"
	"testing"
)

func TestSetPreservesOrderAndRemovesDuplicates(t *testing.T) {
	selected := NewSet(BrowserTask, InternetSearch, BrowserTask, " ")
	selected.Add(DesktopAction, InternetSearch)

	want := []string{BrowserTask, InternetSearch, DesktopAction}
	if got := selected.IDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("IDs() = %v, want %v", got, want)
	}
}

func TestSetFiltersByCapabilityTaxonomy(t *testing.T) {
	selected := NewSet(InternetSearch, BrowserTask, BrowserAutomation, DesktopAction)
	selected.RemoveMatching(IsBrowser)

	want := []string{InternetSearch, DesktopAction}
	if got := selected.IDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("IDs() = %v, want %v", got, want)
	}
	if selected.Contains(BrowserTask) {
		t.Fatal("removed browser capability remains in membership index")
	}
}

func TestCapabilityNamespaceUsesCompleteFirstSegment(t *testing.T) {
	tests := []struct {
		id        string
		namespace Namespace
		browser   bool
		desktop   bool
	}{
		{id: BrowserTask, namespace: NamespaceBrowser, browser: true},
		{id: DesktopAction, namespace: NamespaceDesktop, desktop: true},
		{id: "browserish.task", namespace: "browserish"},
		{id: " browser.observe ", namespace: NamespaceBrowser, browser: true},
	}
	for _, test := range tests {
		if got := NamespaceOf(test.id); got != test.namespace {
			t.Errorf("NamespaceOf(%q) = %q, want %q", test.id, got, test.namespace)
		}
		if got := IsBrowser(test.id); got != test.browser {
			t.Errorf("IsBrowser(%q) = %v, want %v", test.id, got, test.browser)
		}
		if got := IsDesktop(test.id); got != test.desktop {
			t.Errorf("IsDesktop(%q) = %v, want %v", test.id, got, test.desktop)
		}
	}
}

func TestBrowserIDsFollowRegisteredNamespace(t *testing.T) {
	ids := NewSet(BrowserIDs()...)
	for _, want := range []string{BrowserTask, BrowserWait, BrowserDownload, BrowserScreenshot, BrowserAutomation} {
		if !ids.Contains(want) {
			t.Fatalf("BrowserIDs() missing %s: %v", want, ids.IDs())
		}
	}
	for _, id := range ids.IDs() {
		if !IsBrowser(id) {
			t.Fatalf("BrowserIDs() included non-browser capability %s", id)
		}
	}
}

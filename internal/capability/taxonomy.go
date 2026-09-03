package capability

import "strings"

type Namespace string

const (
	NamespaceBrowser Namespace = "browser"
	NamespaceDesktop Namespace = "desktop"
)

// NamespaceOf returns the first segment of a canonical dotted capability ID.
func NamespaceOf(id string) Namespace {
	id = strings.TrimSpace(id)
	if separator := strings.IndexByte(id, '.'); separator >= 0 {
		id = id[:separator]
	}
	return Namespace(id)
}

func IsBrowser(id string) bool { return NamespaceOf(id) == NamespaceBrowser }

func IsDesktop(id string) bool { return NamespaceOf(id) == NamespaceDesktop }

func (r *Registry) IDsInNamespace(namespace Namespace) []string {
	definitions := r.List()
	ids := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		if NamespaceOf(definition.ID) == namespace {
			ids = append(ids, definition.ID)
		}
	}
	return ids
}

func BrowserIDs() []string { return GlobalRegistry.IDsInNamespace(NamespaceBrowser) }

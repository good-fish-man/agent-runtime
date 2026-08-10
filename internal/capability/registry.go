// Package capability owns the stable abilities exposed by the Runtime.
// Concrete tools are implementation details bound through provider adapters.
package capability

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	toolimpl "github.com/good-fish-man/agent-runtime/internal/tools"
)

type Status string

const (
	StatusAvailable   Status = "available"
	StatusUnavailable Status = "unavailable"
)

// Definition is a provider-independent capability contract.
type Definition struct {
	ID          string            `json:"id" yaml:"id"`
	Description string            `json:"description" yaml:"description"`
	Input       map[string]string `json:"input,omitempty" yaml:"input,omitempty"`
	Output      string            `json:"output,omitempty" yaml:"output,omitempty"`
	ReadOnly    bool              `json:"read_only" yaml:"read_only"`
	Risk        string            `json:"risk" yaml:"risk"`
	Status      Status            `json:"status" yaml:"status"`
	Provider    string            `json:"provider,omitempty" yaml:"provider,omitempty"`
	Reason      string            `json:"reason,omitempty" yaml:"reason,omitempty"`
}

type Factory func(basePath string) (tool.BaseTool, error)

type registration struct {
	definition Definition
	factory    Factory
}

type Registry struct {
	mu      sync.RWMutex
	entries map[string]registration
}

func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]registration)}
}

var GlobalRegistry = NewRegistry()

func (r *Registry) Register(definition Definition, factory Factory) error {
	definition.ID = strings.TrimSpace(definition.ID)
	if definition.ID == "" {
		return fmt.Errorf("capability id is required")
	}
	if definition.Risk == "" {
		definition.Risk = "low"
	}
	if factory == nil {
		definition.Status = StatusUnavailable
	} else {
		definition.Status = StatusAvailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[definition.ID]; exists {
		return fmt.Errorf("capability %s is already registered", definition.ID)
	}
	r.entries[definition.ID] = registration{definition: definition, factory: factory}
	return nil
}

func (r *Registry) Get(id string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[id]
	return entry.definition, ok
}

func (r *Registry) List() []Definition {
	r.mu.RLock()
	definitions := make([]Definition, 0, len(r.entries))
	for _, entry := range r.entries {
		definitions = append(definitions, entry.definition)
	}
	r.mu.RUnlock()
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].ID < definitions[j].ID })
	return definitions
}

func (r *Registry) FindByModelName(name string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, entry := range r.entries {
		if ModelName(entry.definition.ID) == name {
			return entry.definition, true
		}
	}
	return Definition{}, false
}

// Resolve creates model-callable adapters for available capability IDs.
func (r *Registry) Resolve(basePath string, ids []string) ([]tool.BaseTool, []string, error) {
	resolved := make([]tool.BaseTool, 0, len(ids))
	unavailable := make([]string, 0)
	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		r.mu.RLock()
		entry, ok := r.entries[id]
		r.mu.RUnlock()
		if !ok || entry.factory == nil || entry.definition.Status != StatusAvailable {
			unavailable = append(unavailable, id)
			continue
		}
		provider, err := entry.factory(basePath)
		if err != nil {
			return nil, unavailable, fmt.Errorf("resolve capability %s: %w", id, err)
		}
		resolved = append(resolved, Wrap(entry.definition, provider))
	}
	return resolved, unavailable, nil
}

// ModelName encodes a dotted capability ID as an API-compatible function name.
func ModelName(id string) string {
	return strings.NewReplacer(".", "_", "-", "_").Replace(id)
}

// IsClientBound reports whether a capability executes on the user's device
// rather than on the server. Client-bound tools return an actionprotocol
// payload that must be dispatched through an OnAction sink, so they can only be
// fulfilled by the streaming execution path.
func IsClientBound(id string) bool {
	return strings.HasPrefix(id, "browser.") || strings.HasPrefix(id, "desktop.")
}

// ClientBoundModelNames returns the model function names of every registered
// client-bound capability. Used to strip these tools from non-streaming runs,
// where their actions cannot be dispatched.
func (r *Registry) ClientBoundModelNames() []string {
	r.mu.RLock()
	names := make([]string, 0)
	for id := range r.entries {
		if IsClientBound(id) {
			names = append(names, ModelName(id))
		}
	}
	r.mu.RUnlock()
	sort.Strings(names)
	return names
}

// ClientBoundModelNames returns the model function names of every registered
// client-bound capability in the global registry.
func ClientBoundModelNames() []string {
	return GlobalRegistry.ClientBoundModelNames()
}

// Wrap exposes a concrete provider through a capability contract.
func Wrap(definition Definition, provider tool.BaseTool) tool.BaseTool {
	if provider == nil {
		return nil
	}
	modelName := ModelName(definition.ID)
	if info, err := provider.Info(context.Background()); err == nil && info != nil {
		_ = toolimpl.GlobalRegistry.RegisterAlias(modelName, info.Name)
	}
	return &boundTool{definition: definition, provider: provider}
}

type boundTool struct {
	definition Definition
	provider   tool.BaseTool
}

func (b *boundTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := b.provider.Info(ctx)
	if err != nil {
		return nil, err
	}
	copy := *info
	copy.Name = ModelName(b.definition.ID)
	copy.Desc = fmt.Sprintf("Capability %s: %s", b.definition.ID, b.definition.Description)
	return &copy, nil
}

func (b *boundTool) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	invokable, ok := b.provider.(tool.InvokableTool)
	if !ok {
		return "", fmt.Errorf("capability %s provider is not invokable", b.definition.ID)
	}
	return invokable.InvokableRun(ctx, input, opts...)
}

func (b *boundTool) ValidateInput(ctx context.Context, input string) *toolimpl.ValidationResult {
	if validator, ok := b.provider.(toolimpl.OptionalValidateTool); ok {
		return validator.ValidateInput(ctx, input)
	}
	return &toolimpl.ValidationResult{Valid: true}
}

func toolFactory(name string) Factory {
	return func(basePath string) (tool.BaseTool, error) {
		providers := toolimpl.ToolsByNamesWithBasePath(basePath, []string{name})
		if len(providers) != 1 {
			return nil, fmt.Errorf("tool provider %s is not available", name)
		}
		return providers[0], nil
	}
}

func mustRegister(definition Definition, provider string) {
	var factory Factory
	if provider != "" {
		if definition.Provider == "" {
			definition.Provider = "builtin"
		}
		factory = toolFactory(provider)
	}
	if err := GlobalRegistry.Register(definition, factory); err != nil {
		panic(err)
	}
}

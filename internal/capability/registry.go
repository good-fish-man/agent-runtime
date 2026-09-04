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
	protocol "github.com/good-fish-man/athena-protocol/protocol/v5"
)

type Status string

const (
	StatusAvailable   Status = "available"
	StatusUnavailable Status = "unavailable"
)

// Definition is a provider-independent capability contract.
type Definition struct {
	ID                  string                    `json:"id" yaml:"id"`
	Description         string                    `json:"description" yaml:"description"`
	Input               map[string]string         `json:"input,omitempty" yaml:"input,omitempty"`
	Output              string                    `json:"output,omitempty" yaml:"output,omitempty"`
	ReadOnly            bool                      `json:"read_only" yaml:"read_only"`
	Risk                string                    `json:"risk" yaml:"risk"`
	RiskFloor           string                    `json:"risk_floor,omitempty" yaml:"risk_floor,omitempty"`
	Status              Status                    `json:"status" yaml:"status"`
	Provider            string                    `json:"provider,omitempty" yaml:"provider,omitempty"`
	ProviderVersion     string                    `json:"provider_version,omitempty" yaml:"provider_version,omitempty"`
	Permissions         map[string]any            `json:"permissions,omitempty" yaml:"permissions,omitempty"`
	ObservationContract string                    `json:"observation_contract,omitempty" yaml:"observation_contract,omitempty"`
	Preconditions       []protocol.WorldCondition `json:"preconditions" yaml:"preconditions"`
	ExpectedEffects     []protocol.WorldEffect    `json:"expected_effects" yaml:"expected_effects"`
	Postconditions      []protocol.WorldCondition `json:"postconditions" yaml:"postconditions"`
	Reason              string                    `json:"reason,omitempty" yaml:"reason,omitempty"`
}

type Factory func(basePath string) (tool.BaseTool, error)

type registration struct {
	definition Definition
	factory    Factory
	external   bool
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
	return r.register(definition, factory, false)
}

// RegisterExternal registers a verified provider loaded from the private
// plugin Registry. External entries are removable as one unit during a safe
// reload; built-in registrations are never replaced.
func (r *Registry) RegisterExternal(definition Definition, factory Factory) error {
	if strings.TrimSpace(definition.Provider) == "" || strings.TrimSpace(definition.ProviderVersion) == "" {
		return fmt.Errorf("external capability requires provider and provider version")
	}
	return r.register(definition, factory, true)
}

func (r *Registry) register(definition Definition, factory Factory, external bool) error {
	definition.ID = strings.TrimSpace(definition.ID)
	if definition.ID == "" {
		return fmt.Errorf("capability id is required")
	}
	if definition.Risk == "" {
		definition.Risk = "low"
	}
	definition = withWorldContract(definition)
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
	r.entries[definition.ID] = registration{definition: definition, factory: factory, external: external}
	return nil
}

func withWorldContract(definition Definition) Definition {
	if len(definition.Preconditions) == 0 {
		definition.Preconditions = []protocol.WorldCondition{{Path: "/runtime/capabilities/" + definition.ID, Operator: "available", Required: true}}
	}
	if len(definition.ExpectedEffects) == 0 && !definition.ReadOnly {
		definition.ExpectedEffects = []protocol.WorldEffect{{Operation: "set", Path: "/capability_effects/" + definition.ID}}
	}
	if len(definition.Postconditions) == 0 {
		definition.Postconditions = []protocol.WorldCondition{{Path: "/observations/latest/status", Operator: "in", Value: []string{"SUCCEEDED", "FAILED", "BLOCKED"}, Required: true}}
	}
	return definition
}

// RemoveExternal drops only dynamically loaded provider registrations. It is
// used before config reload so disabled/revoked versions cannot remain live.
func (r *Registry) RemoveExternal() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, entry := range r.entries {
		if entry.external {
			delete(r.entries, id)
		}
	}
}

// RemoveExternalProvider rolls back a partially registered provider package.
func (r *Registry) RemoveExternalProvider(provider, version string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, entry := range r.entries {
		if entry.external && entry.definition.Provider == provider && entry.definition.ProviderVersion == version {
			delete(r.entries, id)
		}
	}
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
	return IsBrowser(id) || IsDesktop(id)
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

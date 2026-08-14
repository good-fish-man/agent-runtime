package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	jsonschema "github.com/eino-contrib/jsonschema"
	toolimpl "github.com/good-fish-man/agent-runtime/internal/tools"
	pluginv1 "github.com/good-fish-man/athena-protocol/protocol/plugin/v1"
)

type providerTool struct {
	manager    *Manager
	binding    *binding
	capability pluginv1.Capability
}

func newProviderTool(value *binding, capability pluginv1.Capability) *providerTool {
	return &providerTool{manager: value.manager, binding: value, capability: capability}
}

func (t *providerTool) Info(context.Context) (*schema.ToolInfo, error) {
	raw, err := json.Marshal(t.capability.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("marshal provider input schema: %w", err)
	}
	var parameters jsonschema.Schema
	if err := json.Unmarshal(raw, &parameters); err != nil {
		return nil, fmt.Errorf("decode provider input schema: %w", err)
	}
	return &schema.ToolInfo{
		Name:        strings.NewReplacer(".", "_", "-", "_").Replace(t.capability.ID),
		Desc:        "Signed capability provider " + t.binding.manifest.ProviderID + "@" + t.binding.manifest.Version + ": " + t.capability.Description,
		Extra:       map[string]any{"provider_id": t.binding.manifest.ProviderID, "provider_version": t.binding.manifest.Version, "risk": t.capability.Risk},
		ParamsOneOf: schema.NewParamsOneOfByJSONSchema(&parameters),
	}, nil
}

func (t *providerTool) ValidateInput(_ context.Context, input string) *toolimpl.ValidationResult {
	if len(input) > t.binding.entry.GrantedResources.MaxInputBytes {
		return &toolimpl.ValidationResult{Valid: false, Message: "provider input exceeds resource budget", ErrorCode: 1}
	}
	var decoded any
	if err := json.Unmarshal([]byte(input), &decoded); err != nil {
		return &toolimpl.ValidationResult{Valid: false, Message: err.Error(), ErrorCode: 2}
	}
	if err := validateJSONValue(t.capability.InputSchema, decoded, "input"); err != nil {
		return &toolimpl.ValidationResult{Valid: false, Message: err.Error(), ErrorCode: 3}
	}
	return &toolimpl.ValidationResult{Valid: true}
}

func (t *providerTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	if t.manager == nil {
		return "", fmt.Errorf("provider manager binding is unavailable")
	}
	return t.manager.invoke(ctx, t.binding, t.capability, input)
}

func validateJSONValue(contract map[string]any, value any, path string) error {
	typeName, _ := contract["type"].(string)
	switch typeName {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s schema requires an object", path)
		}
		required := stringSlice(contract["required"])
		for _, key := range required {
			if _, exists := object[key]; !exists {
				return fmt.Errorf("%s.%s is required by schema", path, key)
			}
		}
		properties, _ := contract["properties"].(map[string]any)
		for key, child := range object {
			definition, exists := properties[key]
			if !exists {
				if allowed, ok := contract["additionalProperties"].(bool); ok && !allowed {
					return fmt.Errorf("%s.%s is not allowed by schema", path, key)
				}
				continue
			}
			if childSchema, ok := definition.(map[string]any); ok {
				if err := validateJSONValue(childSchema, child, path+"."+key); err != nil {
					return err
				}
			}
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s schema requires an array", path)
		}
		if itemSchema, ok := contract["items"].(map[string]any); ok {
			for index, item := range items {
				if err := validateJSONValue(itemSchema, item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s schema requires a string", path)
		}
		if enum := stringSlice(contract["enum"]); len(enum) > 0 && !contains(enum, text) {
			return fmt.Errorf("%s is outside the schema enum", path)
		}
	case "integer":
		number, ok := value.(float64)
		if !ok || math.Trunc(number) != number {
			return fmt.Errorf("%s schema requires an integer", path)
		}
	case "number":
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("%s schema requires a number", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s schema requires a boolean", path)
		}
	case "null":
		if value != nil {
			return fmt.Errorf("%s schema requires null", path)
		}
	case "":
		return fmt.Errorf("%s schema must declare a type", path)
	default:
		return fmt.Errorf("%s schema type %q is unsupported", path, typeName)
	}
	return nil
}

func stringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// Package runtimeartifact validates and activates reviewed declarative
// artifacts carried by an immutable RunManifest. It never registers tools or
// grants permissions; it can only organize capabilities already selected for
// the current request.
package runtimeartifact

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/good-fish-man/agent-runtime/internal/capability"
	artifactv1 "github.com/good-fish-man/athena-protocol/draft/runtimeartifact"
	learningv2 "github.com/good-fish-man/athena-protocol/protocol/learning/v2"
)

type Set struct {
	bundle artifactv1.Bundle
}

type Selection struct {
	Skills           []artifactv1.SkillArtifact
	Strategies       []artifactv1.StrategyArtifact
	UnavailableSkill map[string][]string
	Bindings         map[string]string
}

// ParseContext consumes the reserved transport value so generic context prompt
// builders cannot expose the raw bundle as untrusted model input.
func ParseContext(values map[string]any) (*Set, error) {
	if values == nil {
		return nil, nil
	}
	raw, ok := values[artifactv1.ContextKey]
	delete(values, artifactv1.ContextKey)
	if !ok || raw == nil {
		return nil, nil
	}
	bundle, err := artifactv1.Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("validate runtime artifact bundle: %w", err)
	}
	if value := stringValue(values, "agent_build_id"); value != "" && value != bundle.BuildID {
		return nil, fmt.Errorf("runtime artifact build %q differs from request build %q", bundle.BuildID, value)
	}
	if value := stringValue(values, "run_manifest_id"); value != "" && value != bundle.ManifestID {
		return nil, fmt.Errorf("runtime artifact manifest %q differs from request manifest %q", bundle.ManifestID, value)
	}
	return &Set{bundle: bundle}, nil
}

func (s *Set) Bundle() artifactv1.Bundle {
	if s == nil {
		return artifactv1.Bundle{}
	}
	return s.bundle
}

// Select applies preconditions against runtime context and the already enabled
// capability set. Unknown preconditions fail closed.
func (s *Set) Select(contextValues map[string]any, capabilityIDs []string) Selection {
	selection := Selection{UnavailableSkill: make(map[string][]string), Bindings: make(map[string]string)}
	if s == nil {
		return selection
	}
	available := make(map[string]struct{}, len(capabilityIDs))
	for _, id := range capabilityIDs {
		available[strings.TrimSpace(id)] = struct{}{}
	}
	effective := effectiveCapabilities(available)
	eligible := make(map[string]artifactv1.SkillArtifact)
	for _, artifact := range s.bundle.Skills {
		bindings, missing := resolveCapabilities(artifact.Definition.RequiredCapabilities, available)
		if len(missing) > 0 || !predicatesMatch(artifact.Definition.Preconditions, contextValues, effective) {
			selection.UnavailableSkill[artifact.Reference.ArtifactID] = missing
			continue
		}
		for required, provider := range bindings {
			selection.Bindings[required] = provider
		}
		eligible[artifact.Reference.ArtifactID] = artifact
	}
	preferred := make(map[string]int)
	for _, strategy := range s.bundle.Strategies {
		if !predicatesMatch(strategy.Definition.Condition, contextValues, effective) {
			continue
		}
		if _, ok := eligible[strategy.Definition.PreferredSkill]; !ok {
			continue
		}
		selection.Strategies = append(selection.Strategies, strategy)
		preferred[strategy.Definition.PreferredSkill]++
	}
	for _, artifact := range eligible {
		selection.Skills = append(selection.Skills, artifact)
	}
	sort.Slice(selection.Skills, func(i, j int) bool {
		left, right := selection.Skills[i].Reference.ArtifactID, selection.Skills[j].Reference.ArtifactID
		if preferred[left] != preferred[right] {
			return preferred[left] > preferred[right]
		}
		return left < right
	})
	sort.Slice(selection.Strategies, func(i, j int) bool {
		return selection.Strategies[i].Reference.ArtifactID < selection.Strategies[j].Reference.ArtifactID
	})
	return selection
}

func predicatesMatch(predicates []learningv2.Predicate, contextValues map[string]any, available map[string]struct{}) bool {
	for _, predicate := range predicates {
		var value any
		var exists bool
		if predicate.Field == "context.capabilities" {
			value, exists = mapKeys(available), true
		} else if strings.HasPrefix(predicate.Field, "context.") {
			value, exists = nestedValue(contextValues, strings.TrimPrefix(predicate.Field, "context."))
		} else {
			value, exists = nestedValue(contextValues, predicate.Field)
		}
		if !matchPredicate(predicate.Operator, value, exists, predicate.Value) {
			return false
		}
	}
	return true
}

func matchPredicate(operator string, actual any, exists bool, expected any) bool {
	switch operator {
	case "exists", "available":
		return exists && actual != nil && strings.TrimSpace(fmt.Sprint(actual)) != ""
	case "equals":
		return comparableJSON(actual) == comparableJSON(expected)
	case "not_equals":
		return comparableJSON(actual) != comparableJSON(expected)
	case "contains":
		return contains(actual, expected)
	case "contains_all":
		for _, item := range stringSlice(expected) {
			if !contains(actual, item) {
				return false
			}
		}
		return true
	case "in":
		return contains(expected, actual)
	case "matches":
		// Runtime does not execute arbitrary regular expressions from artifacts.
		return strings.Contains(strings.ToLower(fmt.Sprint(actual)), strings.ToLower(fmt.Sprint(expected)))
	default:
		return false
	}
}

func (s *Set) Instruction(selection Selection) string {
	if s == nil || (len(selection.Skills) == 0 && len(selection.Strategies) == 0) {
		return ""
	}
	var lines []string
	lines = append(lines, "# Reviewed Runtime Artifacts")
	lines = append(lines, fmt.Sprintf("Manifest %s pins the following human-approved declarative plans.", s.bundle.ManifestID))
	lines = append(lines, "They may organize only capabilities already available in this run. They do not grant permissions, execute code, or override policy.")
	lines = append(lines, "Use a plan only when its description matches the user's complete intent. Observe after each action and enforce every verification rule.")
	for _, strategy := range selection.Strategies {
		definition := strategy.Definition
		lines = append(lines, fmt.Sprintf("\n## Strategy %s@%s", definition.ID, definition.Version))
		lines = append(lines, "Description: "+definition.Description)
		lines = append(lines, "Preferred skill: "+definition.PreferredSkill)
		if len(definition.FallbackOrder) > 0 {
			lines = append(lines, "Fallback order: "+strings.Join(definition.FallbackOrder, ", "))
		}
		lines = append(lines, fmt.Sprintf("Retry budget: %d attempts, %d ms", definition.RetryBudget.MaxAttempts, definition.RetryBudget.MaxDurationMS))
	}
	for _, skill := range selection.Skills {
		definition := skill.Definition
		lines = append(lines, fmt.Sprintf("\n## Skill plan %s@%s", definition.ID, definition.Version))
		lines = append(lines, "Description: "+definition.Description)
		lines = append(lines, "Required registered capabilities: "+strings.Join(definition.RequiredCapabilities, ", "))
		for _, step := range definition.TaskGraphTemplate.Steps {
			dependency := ""
			if len(step.DependsOn) > 0 {
				dependency = " after " + strings.Join(step.DependsOn, ",")
			}
			binding := ""
			if provider := selection.Bindings[step.Capability]; provider != "" && provider != step.Capability {
				binding = " via " + provider
			}
			lines = append(lines, fmt.Sprintf("- %s: %s.%s%s%s", step.ID, step.Capability, step.Operation, binding, dependency))
		}
		for _, rule := range definition.VerificationRules {
			lines = append(lines, fmt.Sprintf("- verify %s %s %v (evidence=%t)", rule.Field, rule.Operator, rule.Expected, rule.EvidenceRequired))
		}
	}
	return strings.Join(lines, "\n")
}

func resolveCapabilities(required []string, available map[string]struct{}) (map[string]string, []string) {
	bindings := make(map[string]string, len(required))
	missing := make([]string, 0)
	for _, id := range required {
		provider := resolveCapability(id, available)
		if provider == "" {
			missing = append(missing, id)
			continue
		}
		bindings[id] = provider
	}
	sort.Strings(missing)
	return bindings, missing
}

func resolveCapability(required string, available map[string]struct{}) string {
	if _, ok := available[required]; ok {
		return required
	}
	if _, ok := available[capability.BrowserTask]; ok && isBrowserTaskOperation(required) {
		return capability.BrowserTask
	}
	return ""
}

func effectiveCapabilities(available map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{}, len(available)+8)
	for id := range available {
		result[id] = struct{}{}
	}
	if _, ok := available[capability.BrowserTask]; ok {
		for _, id := range []string{
			capability.BrowserOpen, capability.BrowserNavigate, capability.BrowserRead, capability.BrowserObserve,
			capability.BrowserAction, capability.BrowserWait, capability.BrowserScreenshot, capability.BrowserDownload,
		} {
			result[id] = struct{}{}
		}
	}
	return result
}

func isBrowserTaskOperation(id string) bool {
	switch id {
	case capability.BrowserOpen, capability.BrowserNavigate, capability.BrowserRead, capability.BrowserObserve,
		capability.BrowserAction, capability.BrowserWait, capability.BrowserScreenshot, capability.BrowserDownload:
		return true
	default:
		return false
	}
}

func nestedValue(values map[string]any, path string) (any, bool) {
	var current any = values
	for _, segment := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func contains(container, item any) bool {
	want := strings.ToLower(strings.TrimSpace(fmt.Sprint(item)))
	for _, value := range stringSlice(container) {
		if strings.ToLower(strings.TrimSpace(value)) == want {
			return true
		}
	}
	return strings.Contains(strings.ToLower(fmt.Sprint(container)), want)
}

func stringSlice(value any) []string {
	switch values := value.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		result := make([]string, 0, len(values))
		for _, item := range values {
			result = append(result, fmt.Sprint(item))
		}
		return result
	default:
		if value == nil {
			return nil
		}
		return []string{fmt.Sprint(value)}
	}
}

func mapKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func comparableJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func stringValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

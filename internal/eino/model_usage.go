package eino

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// ModelIdentity identifies the concrete model behind one provider call.
type ModelIdentity struct {
	ModelID  string
	Provider string
	Model    string
}

// ModelUsageRecord is request-scoped usage aggregated for one concrete model.
type ModelUsageRecord struct {
	ModelID          string
	Provider         string
	Model            string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	RequestCount     int
}

type usageCollectorContextKey struct{}

// UsageCollector safely aggregates model calls made by the main Agent,
// research helpers, context compression, and Sub-Agents in one request.
type UsageCollector struct {
	mu      sync.Mutex
	records map[string]*ModelUsageRecord
}

// WithUsageCollector installs a fresh request-scoped model usage collector.
func WithUsageCollector(ctx context.Context) (context.Context, *UsageCollector) {
	if ctx == nil {
		ctx = context.Background()
	}
	collector := &UsageCollector{records: make(map[string]*ModelUsageRecord)}
	return context.WithValue(ctx, usageCollectorContextKey{}, collector), collector
}

// UsageCollectorFromContext returns the request collector when one is installed.
func UsageCollectorFromContext(ctx context.Context) *UsageCollector {
	if ctx == nil {
		return nil
	}
	collector, _ := ctx.Value(usageCollectorContextKey{}).(*UsageCollector)
	return collector
}

func recordModelUsage(ctx context.Context, identity ModelIdentity, usage Usage) {
	if collector := UsageCollectorFromContext(ctx); collector != nil {
		collector.Record(identity, usage)
	}
}

// Record adds one completed or attempted provider call to the model aggregate.
func (c *UsageCollector) Record(identity ModelIdentity, usage Usage) {
	if c == nil {
		return
	}
	identity.ModelID = strings.TrimSpace(identity.ModelID)
	identity.Provider = strings.TrimSpace(identity.Provider)
	identity.Model = strings.TrimSpace(identity.Model)
	if identity.ModelID == "" && identity.Model == "" {
		return
	}
	if usage.TotalTokens <= 0 && (usage.PromptTokens > 0 || usage.CompletionTokens > 0) {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	key := modelUsageKey(identity)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.records == nil {
		c.records = make(map[string]*ModelUsageRecord)
	}
	record := c.records[key]
	if record == nil {
		record = &ModelUsageRecord{
			ModelID: identity.ModelID, Provider: identity.Provider, Model: identity.Model,
		}
		c.records[key] = record
	}
	if record.ModelID == "" {
		record.ModelID = identity.ModelID
	}
	if record.Provider == "" {
		record.Provider = identity.Provider
	}
	if record.Model == "" {
		record.Model = identity.Model
	}
	record.PromptTokens += usage.PromptTokens
	record.CompletionTokens += usage.CompletionTokens
	record.TotalTokens += usage.TotalTokens
	record.RequestCount++
}

// Snapshot returns a deterministic copy suitable for transport metadata.
func (c *UsageCollector) Snapshot() []ModelUsageRecord {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]ModelUsageRecord, 0, len(c.records))
	for _, record := range c.records {
		if record != nil {
			result = append(result, *record)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ModelID != result[j].ModelID {
			return result[i].ModelID < result[j].ModelID
		}
		if result[i].Provider != result[j].Provider {
			return result[i].Provider < result[j].Provider
		}
		return result[i].Model < result[j].Model
	})
	return result
}

// Total returns usage across every model recorded for this request.
func (c *UsageCollector) Total() Usage {
	var total Usage
	for _, record := range c.Snapshot() {
		total = addUsage(total, Usage{
			PromptTokens: record.PromptTokens, CompletionTokens: record.CompletionTokens, TotalTokens: record.TotalTokens,
		})
	}
	return total
}

func modelUsageKey(identity ModelIdentity) string {
	if identity.ModelID != "" {
		return "id:" + identity.ModelID
	}
	return "name:" + strings.ToLower(identity.Provider) + "\x00" + strings.ToLower(identity.Model)
}

func modelIdentityFromConfig(cfg ModelConfig) ModelIdentity {
	identity := ModelIdentity{Provider: cfg.Provider, Model: cfg.Name}
	for _, key := range []string{"model_id", "ulid", "id"} {
		if value, ok := cfg.ExtraFields[key].(string); ok && strings.TrimSpace(value) != "" {
			identity.ModelID = strings.TrimSpace(value)
			break
		}
	}
	return identity
}

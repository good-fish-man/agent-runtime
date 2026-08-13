package eino

import (
	"context"
	"sync"
	"testing"
)

func TestUsageCollectorSeparatesSameNameByModelID(t *testing.T) {
	collector := &UsageCollector{records: make(map[string]*ModelUsageRecord)}
	collector.Record(ModelIdentity{ModelID: "model-a", Provider: "openai", Model: "shared"}, Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12})
	collector.Record(ModelIdentity{ModelID: "model-b", Provider: "openai", Model: "shared"}, Usage{PromptTokens: 20, CompletionTokens: 4, TotalTokens: 24})

	records := collector.Snapshot()
	if len(records) != 2 || records[0].ModelID != "model-a" || records[1].ModelID != "model-b" {
		t.Fatalf("same-name models were merged: %+v", records)
	}
	if total := collector.Total(); total.PromptTokens != 30 || total.CompletionTokens != 6 || total.TotalTokens != 36 {
		t.Fatalf("unexpected collector total: %+v", total)
	}
}

func TestUsageCollectorAggregatesConcurrentCalls(t *testing.T) {
	ctx, collector := WithUsageCollector(context.Background())
	identity := ModelIdentity{ModelID: "model-main", Provider: "openai", Model: "gpt-test"}
	const calls = 32
	var group sync.WaitGroup
	for i := 0; i < calls; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			recordModelUsage(ctx, identity, Usage{PromptTokens: 3, CompletionTokens: 1, TotalTokens: 4})
		}()
	}
	group.Wait()

	records := collector.Snapshot()
	if len(records) != 1 || records[0].RequestCount != calls || records[0].TotalTokens != calls*4 {
		t.Fatalf("concurrent usage was not aggregated: %+v", records)
	}
}

func TestModelIdentityFromConfigPrefersModelID(t *testing.T) {
	identity := modelIdentityFromConfig(ModelConfig{
		Provider: "OpenAI", Name: "gpt-test", ExtraFields: map[string]any{"model_id": "model-123"},
	})
	if identity.ModelID != "model-123" || identity.Provider != "OpenAI" || identity.Model != "gpt-test" {
		t.Fatalf("unexpected identity: %+v", identity)
	}
}

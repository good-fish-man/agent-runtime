package server

import (
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/eino"
)

func TestBuildResponseMetadataUsesPerModelCollector(t *testing.T) {
	collector := &eino.UsageCollector{}
	collector.Record(eino.ModelIdentity{ModelID: "main", Provider: "openai", Model: "gpt-main"}, eino.Usage{
		PromptTokens: 20, CompletionTokens: 5, TotalTokens: 25,
	})
	collector.Record(eino.ModelIdentity{ModelID: "sub", Provider: "openai", Model: "gpt-sub"}, eino.Usage{
		PromptTokens: 8, CompletionTokens: 2, TotalTokens: 10,
	})

	metadata := buildResponseMetadata("gpt-main", 42, eino.Usage{TotalTokens: 999}, collector)
	if metadata.GetPromptTokens() != 28 || metadata.GetCompletionTokens() != 7 || metadata.GetTokensUsed() != 35 {
		t.Fatalf("aggregate metadata = %+v", metadata)
	}
	if len(metadata.GetModelUsage()) != 2 {
		t.Fatalf("model usage length = %d, want 2", len(metadata.GetModelUsage()))
	}
	if metadata.GetModelUsage()[0].GetModelId() != "main" || metadata.GetModelUsage()[1].GetModelId() != "sub" {
		t.Fatalf("unexpected model usage: %+v", metadata.GetModelUsage())
	}
}

func TestBuildResponseMetadataFallsBackWithoutCollectorRecords(t *testing.T) {
	metadata := buildResponseMetadata("gpt-legacy", 10, eino.Usage{
		PromptTokens: 9, CompletionTokens: 3, TotalTokens: 12,
	}, nil)
	if metadata.GetTokensUsed() != 12 || metadata.GetPromptTokens() != 9 || metadata.GetCompletionTokens() != 3 {
		t.Fatalf("fallback metadata = %+v", metadata)
	}
	if len(metadata.GetModelUsage()) != 0 {
		t.Fatalf("fallback unexpectedly returned model detail: %+v", metadata.GetModelUsage())
	}
}

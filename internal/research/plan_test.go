package research

import (
	"strings"
	"testing"
	"time"
)

func TestAnalyzeNewsBuildsDateBoundQueries(t *testing.T) {
	now := time.Date(2026, time.July, 31, 16, 30, 0, 0, time.UTC)
	plan := Analyze("帮我查询一下今天有哪些新闻资讯", map[string]any{
		"timezone": "Asia/Tokyo",
		"locale":   "zh-CN",
	}, now)

	if plan.Kind != KindNews {
		t.Fatalf("kind = %q, want %q", plan.Kind, KindNews)
	}
	if plan.Date != "2026-08-01" {
		t.Fatalf("date = %q, want 2026-08-01", plan.Date)
	}
	if len(plan.Queries) != 3 {
		t.Fatalf("queries = %v, want 3", plan.Queries)
	}
	for _, query := range plan.Queries {
		if !strings.Contains(query, plan.Date) {
			t.Fatalf("query %q is not bound to date %s", query, plan.Date)
		}
	}
}

func TestAnalyzeSkipsGenericWeatherWithoutLocationWorkflow(t *testing.T) {
	plan := Analyze("帮我查询今天的天气", nil, time.Now())
	if plan.Kind != KindNone {
		t.Fatalf("kind = %q, want no generic research plan", plan.Kind)
	}
}

func TestAnalyzeTravelAndExplicitResearch(t *testing.T) {
	tests := []struct {
		prompt string
		kind   Kind
	}{
		{"下个月去北海道旅行五天，帮我规划", KindTravel},
		{"请深入调研 Go 当前版本并给出来源", KindResearch},
		{"对比两款适合本地模型的笔记本", KindComparison},
	}
	for _, tt := range tests {
		if got := Analyze(tt.prompt, nil, time.Now()); got.Kind != tt.kind {
			t.Errorf("Analyze(%q).Kind = %q, want %q", tt.prompt, got.Kind, tt.kind)
		}
	}
}

func TestAnalyzeUsesEnglishQueriesForEnglishPrompt(t *testing.T) {
	plan := Analyze("Research the latest Go release with sources", nil, time.Now())
	if plan.Kind != KindResearch || !strings.Contains(plan.Queries[1], "official sources") {
		t.Fatalf("unexpected English plan: %+v", plan)
	}
}

func TestAnalyzeConversationKeepsNewsScopeAcrossShortTurns(t *testing.T) {
	now := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	context := map[string]any{"timezone": "Asia/Tokyo", "locale": "zh-CN"}
	initial := "帮我查询一下今天有哪些新闻资讯"

	refined := AnalyzeConversation("tokyo", []string{initial}, context, now)
	if refined.Kind != KindNews || refined.ResponseLanguage != "Chinese" || !strings.Contains(strings.ToLower(refined.Queries[0]), "tokyo") {
		t.Fatalf("unexpected short refinement plan: %+v", refined)
	}

	followUp := AnalyzeConversation("今天的新闻", []string{initial, "tokyo"}, context, now)
	if followUp.Kind != KindNews || !strings.Contains(strings.ToLower(followUp.Queries[0]), "tokyo") {
		t.Fatalf("news scope was lost: %+v", followUp)
	}
}

func TestAnalyzeConversationTreatsClarificationCardAsRefinement(t *testing.T) {
	now := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	original := "Research the latest FIFA World Cup news with sources"
	answer := "Fetch now?：Yes, fetch now Count：8 (default) Sources：No preference"

	plan := AnalyzeConversation(answer, []string{original}, map[string]any{"locale": "en-US"}, now)
	if plan.Kind != KindNews {
		t.Fatalf("kind = %q, want %q; plan=%+v", plan.Kind, KindNews, plan)
	}
	for _, query := range plan.Queries {
		if !strings.Contains(strings.ToLower(query), "fifa") {
			t.Fatalf("query lost the original subject: %q", query)
		}
		if strings.Contains(strings.ToLower(query), "fetch now?") {
			t.Fatalf("query contains clarification field label: %q", query)
		}
	}
}

func TestAnalyzeConversationDoesNotSearchOrphanClarificationCard(t *testing.T) {
	answer := "Fetch now?：Yes Count：8 Sources：No preference"
	if plan := AnalyzeConversation(answer, nil, nil, time.Now()); plan.Kind != KindNone {
		t.Fatalf("orphan clarification answer became a search plan: %+v", plan)
	}
}

func TestAnalyzeExtractsExactUserURL(t *testing.T) {
	plan := Analyze("总结 https://example.com/report?year=2026。", nil, time.Now())
	if plan.Kind != KindResearch || len(plan.SeedURLs) != 1 || plan.SeedURLs[0] != "https://example.com/report?year=2026" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestAnalyzeRecognizesLearnAboutAsResearch(t *testing.T) {
	plan := Analyze("帮我了解一下 MCP", map[string]any{"locale": "zh-CN"}, time.Now())
	if plan.Kind != KindResearch || len(plan.Queries) < 2 {
		t.Fatalf("learn-about request did not enter research: %+v", plan)
	}
}

func TestAnalyzeOfficialProcedureUsesCurrentAuthoritativeResearch(t *testing.T) {
	now := time.Date(2026, time.August, 10, 9, 0, 0, 0, time.UTC)
	plan := Analyze("我是中国人，在日本工作，想把中国驾照换成日本驾照，我应该怎么做", map[string]any{
		"locale":   "zh-CN",
		"timezone": "Asia/Tokyo",
	}, now)
	if plan.Kind != KindResearch || plan.MinSources < 3 || plan.Date != "2026-08-10" {
		t.Fatalf("unexpected official procedure plan: %+v", plan)
	}
	if len(plan.Queries) < 2 || !strings.Contains(plan.Queries[0], plan.Date) || !strings.Contains(plan.Queries[1], "官方") {
		t.Fatalf("official procedure queries are not date-bound and authority-focused: %+v", plan.Queries)
	}
}

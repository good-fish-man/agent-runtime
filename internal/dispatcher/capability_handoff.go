package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
	"github.com/good-fish-man/agent-runtime/internal/eino"
	"github.com/good-fish-man/agent-runtime/internal/research"
	"github.com/good-fish-man/agent-runtime/internal/tools"
	log "github.com/good-fish-man/logx"
)

const capabilityHandoffSchema = "athena.capability-handoff.v3"

// dispatchCapabilityHandoff resumes a browser task only after the independent
// Search System has resolved the exact public URL. Browser Runtime never
// performs web search itself, while the user still gets one continuous task.
func (d *Dispatcher) dispatchCapabilityHandoff(
	ctx context.Context,
	userPrompt string,
	emitChunk func(eino.StreamChunk) error,
	emitAction func(actionprotocol.Action) error,
) (*eino.Result, bool, error) {
	observation, _, ok := deviceObservationPayload(d.req.Context, userPrompt)
	if !ok || !strings.EqualFold(strings.TrimSpace(stringValue(observation["status"])), "SUCCEEDED") {
		return nil, false, nil
	}
	state, ok := mapValue(observation["state"])
	if !ok {
		return nil, false, nil
	}
	handoff, ok := mapValue(state["capability_handoff"])
	if !ok || stringValue(handoff["schema"]) != capabilityHandoffSchema ||
		stringValue(handoff["from"]) != "browser.task" || stringValue(handoff["to"]) != "internet.search" {
		return nil, false, nil
	}
	query := strings.TrimSpace(stringValue(handoff["query"]))
	resume, ok := mapValue(handoff["resume"])
	if query == "" || !ok {
		return nil, true, fmt.Errorf("dispatcher.capabilityHandoff: invalid browser-to-search handoff payload")
	}
	if d.researchExecutor == nil {
		return d.capabilityHandoffFailure(emitChunk, "Search capability is unavailable; the browser target URL could not be resolved.")
	}

	plan := research.Analyze("Search online for the official website: "+query, d.req.Context, time.Now())
	if plan.Kind == research.KindNone {
		plan.Kind = research.KindResearch
	}
	plan.Queries = []string{query}
	plan.ResolvedRequest = query
	plan.MinSources, plan.MaxSources = 1, 3
	started := time.Now()
	evidence, err := d.researchExecutor.Execute(ctx, plan)
	if err != nil {
		return nil, true, log.WrapError(err, "dispatcher.capabilityHandoff.search")
	}
	source, ok := selectBrowserHandoffSource(evidence.Sources, query)
	if !ok {
		log.Warnw(ctx, "browser target handoff returned no safe URL", "query", query, "sources", len(evidence.Sources), "elapsed_ms", time.Since(started).Milliseconds())
		return d.capabilityHandoffFailure(emitChunk, "Search completed, but no safe exact website URL could be verified for the browser task.")
	}
	resolvedURL := browserHandoffRootURL(source.URL)
	if resolvedURL == "" {
		return nil, true, fmt.Errorf("dispatcher.capabilityHandoff: selected source has no safe browser URL")
	}

	contextual, _ := resume["contextual_media_title"].(bool)
	input, err := json.Marshal(tools.BrowserTaskInput{
		SessionID:            strings.TrimSpace(stringValue(handoff["session_id"])),
		Goal:                 strings.TrimSpace(stringValue(resume["goal"])),
		Target:               resolvedURL,
		Query:                strings.TrimSpace(stringValue(resume["query"])),
		ContextualMediaTitle: contextual,
	})
	if err != nil {
		return nil, true, log.WrapError(err, "dispatcher.capabilityHandoff.encode")
	}
	payload, err := tools.NewBrowserTaskTool().InvokableRun(ctx, string(input))
	if err != nil {
		return nil, true, log.WrapError(err, "dispatcher.capabilityHandoff.resume")
	}
	action, ok := actionprotocol.Parse(payload)
	if !ok {
		return nil, true, fmt.Errorf("dispatcher.capabilityHandoff: resumed browser task returned invalid protocol payload")
	}
	if err := emitAction(action); err != nil {
		return nil, true, log.WrapError(err, "dispatcher.capabilityHandoff.emit")
	}
	log.Infow(ctx, "browser capability handoff resumed",
		"query", query, "resolved_url", resolvedURL, "source", source.Title,
		"session_id", action.SessionID, "elapsed_ms", time.Since(started).Milliseconds(),
	)
	return &eino.Result{FinishReason: "client_action", ActionCount: 1}, true, nil
}

func (d *Dispatcher) capabilityHandoffFailure(emit func(eino.StreamChunk) error, message string) (*eino.Result, bool, error) {
	if emit != nil {
		if err := emit(eino.StreamChunk{Text: message}); err != nil {
			return nil, true, log.WrapError(err, "dispatcher.capabilityHandoff.failureEmit")
		}
	}
	return &eino.Result{Content: message, FinishReason: "stop"}, true, nil
}

func selectBrowserHandoffSource(sources []research.Source, query string) (research.Source, bool) {
	queryTerms := browserHandoffTerms(query)
	bestScore := -1.0
	var best research.Source
	for _, source := range sources {
		parsed, err := url.Parse(strings.TrimSpace(source.URL))
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || isSearchProviderHost(parsed.Hostname()) {
			continue
		}
		text := strings.ToLower(parsed.Hostname() + " " + source.Title)
		matches := 0
		for _, term := range queryTerms {
			if strings.Contains(text, term) {
				matches++
			}
		}
		relevance := 0.0
		if len(queryTerms) > 0 {
			relevance = float64(matches) / float64(len(queryTerms))
		}
		score := source.EvidenceScore*0.45 + source.TrustScore*0.25 + source.RelevanceScore*0.15 + relevance*0.15
		if score > bestScore {
			bestScore, best = score, source
		}
	}
	return best, bestScore >= 0
}

func browserHandoffTerms(value string) []string {
	value = strings.ToLower(strings.NewReplacer("-", " ", "_", " ", ".", " ", "/", " ").Replace(value))
	result := make([]string, 0, 4)
	for _, term := range strings.Fields(value) {
		switch term {
		case "official", "website", "site", "home", "page", "the", "官网", "官方", "网站", "首頁", "首页":
			continue
		}
		if len([]rune(term)) >= 2 {
			result = append(result, term)
		}
	}
	return result
}

func isSearchProviderHost(host string) bool {
	host = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(host), "www."))
	for _, provider := range []string{"google.com", "bing.com", "duckduckgo.com", "search.yahoo.com", "baidu.com"} {
		if host == provider || strings.HasSuffix(host, "."+provider) {
			return true
		}
	}
	return false
}

func browserHandoffRootURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return ""
	}
	parsed.Path, parsed.RawPath, parsed.RawQuery, parsed.Fragment = "/", "", "", ""
	return parsed.String()
}

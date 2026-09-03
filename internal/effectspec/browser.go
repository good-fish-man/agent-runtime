// Package effectspec builds effect-centric task semantics before an Action is
// grounded to a concrete desktop actor.
package effectspec

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	semantics "github.com/good-fish-man/athena-protocol/draft/v0alpha"
)

var numericOrdinalPattern = regexp.MustCompile(`(?i)\b([1-9][0-9]*)(?:st|nd|rd|th)?\b`)

func NewBrowserTrace(goal, target, query, sessionID string) *semantics.SemanticTrace {
	now := time.Now().UTC()
	goal = strings.TrimSpace(goal)
	target = strings.TrimSpace(target)
	query = strings.TrimSpace(query)
	sessionID = strings.TrimSpace(sessionID)

	selector := browserSelector(goal, target, query)
	targetSpec := semantics.TargetSpec{
		TargetSpecID:  semantics.NewID("target-spec"),
		CollectionRef: browserCollectionRef(goal),
		Selector:      selector,
	}
	if sessionID != "" {
		targetSpec.SourceSnapshotRef = "browser-session:" + sessionID + ":latest"
	}

	desired := semantics.EffectClause{
		ClauseID: semantics.NewID("effect"), Kind: semantics.EffectDesired,
		Subject: "target.entity", Predicate: "browser.page_state", Operator: "equals", Expected: "available", Required: true,
	}
	operation := "navigate"
	if browserPlaybackRequested(goal) {
		desired.Predicate, desired.Expected, operation = "media.playback_state", "playing", "play"
	} else if browserSelectionRequested(goal) {
		desired.Predicate, desired.Expected, operation = "target.selection_state", "selected", "click"
	}
	preserve := semantics.EffectClause{
		ClauseID: semantics.NewID("effect"), Kind: semantics.EffectMustPreserve,
		Subject: "browser.session", Predicate: "browser.session_identity", Operator: "equals", Expected: sessionID, Required: true,
	}
	preserveAuth := semantics.EffectClause{
		ClauseID: semantics.NewID("effect"), Kind: semantics.EffectMustPreserve,
		Subject: "browser.authentication", Predicate: "browser.authentication_state", Operator: "equals", Expected: "unchanged", Required: true,
	}
	forbidden := semantics.EffectClause{
		ClauseID: semantics.NewID("effect"), Kind: semantics.EffectForbidden,
		Subject: "browser.window", Predicate: "browser.window_closed", Operator: "equals", Expected: false, Required: true,
	}
	outcome := semantics.OutcomeSpec{
		Schema: semantics.Schema, OutcomeID: semantics.NewID("outcome"), Goal: goal, TargetSpec: targetSpec,
		DesiredEffects: []semantics.EffectClause{desired}, MustPreserve: []semantics.EffectClause{preserve, preserveAuth},
		ForbiddenEffects: []semantics.EffectClause{forbidden}, CreatedAt: now,
		Constraints: map[string]any{
			"same_browser_session":               true,
			"require_observed_effect":            true,
			"forbid_unrequested_external_writes": true,
		},
		ActorConstraints: map[string]any{"environment": "browser", "visible": true},
	}
	for _, clause := range []semantics.EffectClause{desired, preserve, preserveAuth, forbidden} {
		outcome.VerificationRequirements = append(outcome.VerificationRequirements, semantics.VerificationRequirement{
			ClauseID: clause.ClauseID, EvidenceKinds: browserEvidenceKinds(clause.Predicate), MinConfidence: 0.72,
		})
	}

	steps := []semantics.PlanStep{
		{StepID: semantics.NewID("plan-step"), Ordinal: 1, Capability: "browser.observe", Operation: "observe", Purpose: "read the current browser world state"},
		{StepID: semantics.NewID("plan-step"), Ordinal: 2, Capability: "browser.task", Operation: "resolve_target", Purpose: "ground the target against observed page entities"},
		{StepID: semantics.NewID("plan-step"), Ordinal: 3, Capability: "browser.task", Operation: operation, Purpose: "apply the requested reversible effect", ExpectedEffectIDs: []string{desired.ClauseID}},
		{StepID: semantics.NewID("plan-step"), Ordinal: 4, Capability: "browser.observe", Operation: "verify", Purpose: "verify effects from post-action evidence", ExpectedEffectIDs: []string{desired.ClauseID, preserve.ClauseID, preserveAuth.ClauseID, forbidden.ClauseID}},
	}
	plan := semantics.PlanCandidate{
		PlanCandidateID: semantics.NewID("plan-candidate"), OutcomeRef: outcome.OutcomeID, Steps: steps, CreatedAt: now,
	}
	plan.DefinitionHash = semantics.Hash(steps)
	return &semantics.SemanticTrace{Schema: semantics.Schema, Outcome: outcome, Plan: plan}
}

func browserSelector(goal, target, query string) semantics.TargetSelector {
	ordinal := browserOrdinal(goal)
	if ordinal > 0 {
		return semantics.TargetSelector{Type: "ordinal", Ordinal: ordinal, Kind: browserEntityKind(goal)}
	}
	if query != "" {
		return semantics.TargetSelector{Type: "query", Value: query, Kind: browserEntityKind(goal)}
	}
	if target != "" {
		return semantics.TargetSelector{Type: "named", Value: target, Kind: browserEntityKind(goal)}
	}
	return semantics.TargetSelector{Type: "current_page", Value: goal, Kind: browserEntityKind(goal)}
}

func browserOrdinal(goal string) int {
	lower := strings.ToLower(goal)
	words := []struct {
		values []string
		value  int
	}{
		{[]string{"first", "第一个", "第一個", "第1个", "第1個"}, 1},
		{[]string{"second", "第两个", "第二个", "第二個", "第2个", "第2個"}, 2},
		{[]string{"third", "第三个", "第三個", "第3个", "第3個"}, 3},
		{[]string{"fourth", "第四个", "第四個", "第4个", "第4個"}, 4},
		{[]string{"fifth", "第五个", "第五個", "第5个", "第5個"}, 5},
	}
	for _, item := range words {
		for _, value := range item.values {
			if strings.Contains(lower, value) {
				return item.value
			}
		}
	}
	match := numericOrdinalPattern.FindStringSubmatch(lower)
	if len(match) == 2 {
		value, _ := strconv.Atoi(match[1])
		return value
	}
	return 0
}

func browserPlaybackRequested(goal string) bool {
	return containsAny(strings.ToLower(goal), "play", "watch", "listen", "播放", "观看", "觀看", "收听", "收聽")
}

func browserSelectionRequested(goal string) bool {
	return containsAny(strings.ToLower(goal), "click", "select", "choose", "press", "点击", "點擊", "选择", "選擇", "按下")
}

func browserEntityKind(goal string) string {
	lower := strings.ToLower(goal)
	switch {
	case containsAny(lower, "video", "movie", "film", "视频", "視頻", "影片", "电影", "電影"):
		return "video"
	case containsAny(lower, "music", "song", "track", "audio", "音乐", "音樂", "歌曲", "音频", "音頻"):
		return "audio"
	case browserSelectionRequested(lower):
		return "control"
	default:
		return "page"
	}
}

func browserCollectionRef(goal string) string {
	kind := browserEntityKind(goal)
	if kind == "video" || kind == "audio" {
		return "current_page." + kind + "_collection"
	}
	return "current_page"
}

func browserEvidenceKinds(predicate string) []string {
	switch predicate {
	case "media.playback_state":
		return []string{"media_state", "post_action_observation"}
	case "target.selection_state":
		return []string{"semantic_target", "page_delta"}
	case "browser.session_identity":
		return []string{"session_state"}
	case "browser.authentication_state":
		return []string{"interaction_trace", "authentication_state"}
	default:
		return []string{"post_action_observation"}
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

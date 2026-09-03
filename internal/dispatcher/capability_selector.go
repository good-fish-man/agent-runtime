package dispatcher

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/good-fish-man/agent-runtime/internal/eino"
	"github.com/good-fish-man/agent-runtime/internal/intent"
	athenarouter "github.com/good-fish-man/agent-runtime/internal/router"
	"github.com/good-fish-man/agent-runtime/internal/types"
)

var (
	skillCreationKeywords = []string{
		"创建技能", "新增技能", "编写技能", "修改技能", "优化技能", "创建skill", "新增skill", "create skill", "build skill", "improve skill", "update skill",
	}
	latinTokenPattern = regexp.MustCompile(`[a-z0-9][a-z0-9._-]*`)
)

func capabilityText(userPrompt string, msgs []eino.ChatMessage) string {
	parts := []string{userPrompt}
	start := len(msgs) - 3
	if start < 0 {
		start = 0
	}
	for _, msg := range msgs[start:] {
		parts = append(parts, msg.Content)
	}
	return strings.ToLower(strings.Join(parts, "\n"))
}

// selectBuiltinCapabilities remains as a small compatibility seam for tests
// and callers that only need capability IDs. Runtime dispatch uses RoutePlan.
func selectBuiltinCapabilities(text string, hasFiles bool) []string {
	parsed := intent.Parse(intent.Request{Text: text, HasFiles: hasFiles})
	return athenarouter.RouteIntent(parsed).Capabilities
}

type scoredSkill struct {
	skill types.Skill
	score int
}

func selectRelevantSkills(skills []types.Skill, text string, limit int) []types.Skill {
	if limit <= 0 || len(skills) == 0 {
		return nil
	}
	text = strings.ToLower(text)
	queryTokens := tokenSet(text)
	queryBigrams := cjkBigrams(text)
	scored := make([]scoredSkill, 0, len(skills))
	for _, skill := range skills {
		metadata := strings.ToLower(strings.Join([]string{skill.ID, skill.Name, skill.Description, skill.Trigger}, " "))
		score := 0
		for _, name := range []string{skill.ID, skill.Name} {
			name = strings.ToLower(strings.TrimSpace(name))
			if name != "" && strings.Contains(text, name) {
				score += 12
			}
		}
		for _, trigger := range strings.FieldsFunc(strings.ToLower(skill.Trigger), func(r rune) bool {
			return r == ',' || r == ';' || r == '，' || r == '；'
		}) {
			trigger = strings.TrimSpace(trigger)
			if utf8.RuneCountInString(trigger) >= 2 && strings.Contains(text, trigger) {
				score += 6
			}
		}
		for token := range queryTokens {
			if len(token) >= 3 && strings.Contains(metadata, token) {
				score += 2
			}
		}
		metadataBigrams := cjkBigrams(metadata)
		for gram := range queryBigrams {
			if metadataBigrams[gram] {
				score++
			}
		}
		if score >= 2 {
			scored = append(scored, scoredSkill{skill: skill, score: score})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].skill.ID < scored[j].skill.ID
		}
		return scored[i].score > scored[j].score
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	result := make([]types.Skill, 0, len(scored))
	for _, item := range scored {
		result = append(result, item.skill)
	}
	return result
}

func matchesAny(text string, keywords []string) bool {
	text = strings.ToLower(text)
	tokens := tokenSet(text)
	for _, keyword := range keywords {
		keyword = strings.ToLower(keyword)
		if isSimpleLatinToken(keyword) {
			if tokens[keyword] {
				return true
			}
			continue
		}
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func isSimpleLatinToken(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func tokenSet(text string) map[string]bool {
	result := make(map[string]bool)
	for _, token := range latinTokenPattern.FindAllString(strings.ToLower(text), -1) {
		result[token] = true
		if normalized := strings.Trim(token, "._-"); normalized != "" {
			result[normalized] = true
		}
	}
	return result
}

func cjkBigrams(text string) map[string]bool {
	result := make(map[string]bool)
	var run []rune
	flush := func() {
		for i := 0; i+1 < len(run); i++ {
			result[string(run[i:i+2])] = true
		}
		run = run[:0]
	}
	for _, r := range text {
		if utf8.RuneLen(r) == 3 && r >= 0x4E00 && r <= 0x9FFF {
			run = append(run, r)
		} else if len(run) > 0 {
			flush()
		}
	}
	if len(run) > 0 {
		flush()
	}
	return result
}

func skillNames(skills []types.Skill) []string {
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		name := skill.ID
		if skill.Name != "" {
			name = skill.Name
		}
		names = append(names, name)
	}
	return names
}

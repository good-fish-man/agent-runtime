package language

import "strings"

// Selection is the response language selected from an explicit user request
// or, by default, the language chosen in the frontend.
type Selection struct {
	Name     string
	Explicit bool
}

// Resolve gives explicit instructions priority over the frontend locale.
func Resolve(locale, prompt string) Selection {
	lower := strings.ToLower(prompt)
	explicit := []struct {
		name    string
		phrases []string
	}{
		{"Chinese", []string{"用中文", "中文回答", "中文回复", "说中文", "in chinese", "answer in chinese", "reply in chinese"}},
		{"English", []string{"用英文", "用英语", "英文回答", "英语回答", "英文回复", "in english", "answer in english", "reply in english"}},
		{"Japanese", []string{"用日文", "用日语", "日文回答", "日语回答", "in japanese", "answer in japanese", "reply in japanese"}},
	}
	for _, candidate := range explicit {
		for _, phrase := range candidate.phrases {
			if strings.Contains(lower, phrase) {
				return Selection{Name: candidate.name, Explicit: true}
			}
		}
	}

	switch {
	case strings.HasPrefix(strings.ToLower(locale), "zh"):
		return Selection{Name: "Chinese"}
	case strings.HasPrefix(strings.ToLower(locale), "ja"):
		return Selection{Name: "Japanese"}
	case strings.HasPrefix(strings.ToLower(locale), "en"):
		return Selection{Name: "English"}
	}
	return Selection{Name: "English"}
}

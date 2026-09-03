package plugins

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/good-fish-man/agent-runtime/internal/types"
	log "github.com/good-fish-man/logx"
)

// LoadSkills builds the effective skill set for a run. It starts from the
// skills declared on the request (filling in an empty Instruction from the
// skill's SKILL.md on disk when available) and then augments the set with any
// skills auto-discovered under skillsDir. Request skills take precedence over
// discovered skills with the same ID.
func LoadSkills(ctx context.Context, reqSkills []types.Skill, skillsDir string) []types.Skill {
	var skills []types.Skill

	for _, s := range reqSkills {
		skill := s
		if skill.Instruction == "" && skillsDir != "" {
			skillPath := filepath.Join(skillsDir, skill.ID, "SKILL.md")
			if data, err := os.ReadFile(skillPath); err == nil {
				skill.Instruction = string(data)
			}
		}
		skills = append(skills, skill)
	}

	if skillsDir != "" {
		if _, err := os.Stat(skillsDir); err == nil {
			for _, ds := range DiscoverSkillsFromDir(ctx, skillsDir) {
				exists := false
				for _, s := range skills {
					if s.ID == ds.ID {
						exists = true
						break
					}
				}
				if !exists {
					skills = append(skills, ds)
				}
			}
		}
	}

	return skills
}

// MergeSkills appends extras that are not already present (by ID) into base and
// returns the combined slice. base entries take precedence.
func MergeSkills(base []types.Skill, extras []types.Skill) []types.Skill {
	for _, e := range extras {
		exists := false
		for _, b := range base {
			if b.ID == e.ID {
				exists = true
				break
			}
		}
		if !exists {
			base = append(base, e)
		}
	}
	return base
}

// DiscoverSkillsFromDir scans a directory for skill folders. Each immediate
// subdirectory that contains a SKILL.md is registered as a skill; its metadata
// is derived from the SKILL.md YAML frontmatter and its layout on disk.
func DiscoverSkillsFromDir(ctx context.Context, dir string) []types.Skill {
	var discovered []types.Skill

	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Warnf(ctx, "[DiscoverSkillsFromDir] failed to read skills dir: %v", err)
		return discovered
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillID := entry.Name()
		skillDir := filepath.Join(dir, skillID)
		skillMD := filepath.Join(skillDir, "SKILL.md")

		data, err := os.ReadFile(skillMD)
		if err != nil {
			continue
		}

		skill := types.Skill{
			ID:          skillID,
			Name:        skillID,
			Instruction: string(data),
			Scope:       "both",
			FilePath:    skillDir,
		}

		if fm := parseSkillFrontmatter(string(data)); fm != nil {
			if v, ok := fm["name"].(string); ok && v != "" {
				skill.Name = v
			}
			if v, ok := fm["description"].(string); ok && v != "" {
				skill.Description = v
			}
			if v, ok := fm["scope"].(string); ok && v != "" {
				skill.Scope = v
			}
			if v, ok := fm["trigger"].(string); ok && v != "" {
				skill.Trigger = v
			}
		}

		if entryScript := discoverEntryScript(skillDir, skillID); entryScript != "" {
			skill.EntryScript = entryScript
		}

		discovered = append(discovered, skill)
		log.Infof(ctx, "[DiscoverSkillsFromDir] discovered skill: %s (name=%s, entry=%s)", skill.ID, skill.Name, skill.EntryScript)
	}

	return discovered
}

// discoverEntryScript locates an entry script under <skillDir>/scripts using the
// priority: <skillID>.sh > main.sh > first *.sh file.
func discoverEntryScript(skillDir, skillID string) string {
	scriptsDir := filepath.Join(skillDir, "scripts")
	info, err := os.Stat(scriptsDir)
	if err != nil || !info.IsDir() {
		return ""
	}

	for _, name := range []string{skillID + ".sh", "main.sh"} {
		if _, err := os.Stat(filepath.Join(scriptsDir, name)); err == nil {
			return name
		}
	}

	if entries, err := os.ReadDir(scriptsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".sh") {
				return e.Name()
			}
		}
	}
	return ""
}

// parseSkillFrontmatter parses the leading "--- ... ---" YAML frontmatter of a
// SKILL.md file into a flat key/value map. Only simple "key: value" lines are
// supported, which is sufficient for skill metadata.
func parseSkillFrontmatter(content string) map[string]any {
	if !strings.HasPrefix(content, "---") {
		return nil
	}

	lines := strings.Split(content, "\n")
	if len(lines) < 3 {
		return nil
	}

	endIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endIdx = i
			break
		}
	}
	if endIdx < 2 {
		return nil
	}

	result := make(map[string]any)
	for i := 1; i < endIdx; i++ {
		line := strings.TrimSpace(lines[i])
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			value = strings.Trim(value, "\"")
			value = strings.Trim(value, "'")
			result[key] = value
		}
	}
	return result
}

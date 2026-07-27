package plugins

import (
	"os"
	"path/filepath"

	"github.com/good-fish-man/agent-runtime/internal/constant"
)

// This file provides the small subset of directory helpers that the skill
// engine needs. It replaces the reference runner's pkg/xqldir package with a
// self-contained, environment-driven implementation so agent-runtime does not
// depend on the XiaoQinglong-specific directory layout.

// GetBaseDir returns the unified base directory used for skills, reports and
// other runtime data.
//
// Priority: AGENT_RUNTIME_HOME > XQL_BASE_DIR > ~/.agent-runtime
func GetBaseDir() string {
	if home := os.Getenv(constant.EnvAgentRuntimeHome); home != "" {
		if filepath.IsAbs(home) {
			return home
		}
		cwd, _ := os.Getwd()
		return filepath.Join(cwd, home)
	}

	if baseDir := os.Getenv(constant.EnvBaseDir); baseDir != "" {
		if filepath.IsAbs(baseDir) {
			return baseDir
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, baseDir)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), constant.DefaultBaseDirName)
	}
	return filepath.Join(home, constant.DefaultBaseDirName)
}

// GetSkillsDir returns the directory that holds skill asset folders
// (each containing a SKILL.md plus scripts/templates/references).
//
// Priority: SKILLS_DIR (env) > <baseDir>/skills
func GetSkillsDir() string {
	if dir := os.Getenv(constant.EnvSkillsDir); dir != "" {
		if filepath.IsAbs(dir) {
			return dir
		}
		return filepath.Join(GetBaseDir(), dir)
	}
	return filepath.Join(GetBaseDir(), constant.DirSkills)
}

// GetReportsDir returns the directory where generated HTML/PPTX reports are stored.
func GetReportsDir() string {
	return filepath.Join(GetBaseDir(), constant.DirDataReports)
}

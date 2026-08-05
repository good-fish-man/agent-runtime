package dispatcher

import (
	"context"
	"os"
	"path/filepath"

	"github.com/good-fish-man/agent-runtime/internal/constant"
	"github.com/good-fish-man/agent-runtime/internal/plugins"
	"github.com/good-fish-man/agent-runtime/internal/types"
	log "github.com/good-fish-man/logx"

	"github.com/cloudwego/eino/components/tool"
)

// buildSkillTools loads the effective skill set for the run and returns the
// skill-related tools (skill execution, load_skill, orchestrate_skills and
// create_skill) to register alongside the other extra tools.
//
// Skills are located under a skills directory resolved from (in priority order)
// the request Context "skills_dir" entry, the request SkillsDir field, a
// "skills" folder next to the working directory, and finally the
// environment-driven default from plugins.GetSkillsDir.
func (d *Dispatcher) buildSkillTools(ctx context.Context) []tool.BaseTool {
	skillsDir := d.skillsDir
	skills := d.req.Skills
	if len(skills) == 0 {
		if d.allowSkillCreation {
			return []tool.BaseTool{plugins.NewCreateSkillToolForDir(skillsDir)}
		}
		return nil
	}

	configPath := d.cfg.SkillsConfigPath
	if configPath == "" {
		configPath = plugins.DefaultSkillConfigPath()
	}
	configMgr, err := plugins.NewSkillConfigManager(configPath)
	if err != nil {
		log.Warnf("[Dispatcher] skills: failed to load skill config: %v", err)
		configMgr, _ = plugins.NewSkillConfigManager("")
	}

	runner := plugins.NewSkillRunner(
		skills,
		skillsDir,
		d.sandboxConfig(),
		d.client.Model(),
		configMgr,
		d.workDir,
	)
	if sessionID, ok := sessionIDFromContext(d.req); ok {
		runner.CurrentSessionID = sessionID
	}

	var out []tool.BaseTool

	if t := runner.BuildSkillTool(); t != nil {
		if info, _ := t.Info(ctx); info != nil {
			out = append(out, t)
		}
	}
	if t := runner.BuildLoadSkillTool(); t != nil {
		if info, _ := t.Info(ctx); info != nil {
			out = append(out, t)
		}
	}

	if len(skills) > 1 {
		planner := plugins.NewSkillPlanner(skills, runner, d.client.Model())
		if t := runner.BuildSkillOrchestratorTool(planner); t != nil {
			if info, _ := t.Info(ctx); info != nil {
				out = append(out, t)
			}
		}
	}

	if d.allowSkillCreation {
		out = append(out, plugins.NewCreateSkillToolForDir(skillsDir))
	}

	log.Infof("[Dispatcher] skills: registered %d skill(s), %d skill tool(s)", len(skills), len(out))
	return out
}

func (d *Dispatcher) discoverSkills() (string, []types.Skill) {
	skillsDir := d.resolveSkillsDir()
	log.Infof("[Dispatcher] skills: using skills_dir: %s", skillsDir)
	skills := plugins.LoadSkills(d.req.Skills, skillsDir)
	if d.cfg.SkillsGlobalDir != "" {
		skills = plugins.MergeSkills(skills, plugins.DiscoverSkillsFromDir(d.cfg.SkillsGlobalDir))
	}
	return skillsDir, skills
}

// resolveSkillsDir determines the directory that holds skill asset folders.
func (d *Dispatcher) resolveSkillsDir() string {
	skillsDir := ""

	if d.req.Context != nil {
		if v, ok := d.req.Context["skills_dir"].(string); ok && v != "" {
			skillsDir = v
		}
	}
	if skillsDir == "" && d.cfg.SkillsDir != "" {
		skillsDir = d.cfg.SkillsDir
	}
	if skillsDir == "" {
		if local := filepath.Join(d.workDir, constant.DirSkills); dirExists(local) {
			skillsDir = local
		}
	}
	if skillsDir == "" {
		skillsDir = plugins.GetSkillsDir()
	}
	if !filepath.IsAbs(skillsDir) {
		skillsDir = filepath.Join(plugins.GetBaseDir(), skillsDir)
	}
	return skillsDir
}

// sandboxConfig returns the per-request sandbox config, applying operator-level
// defaults (image / workdir / timeout) for any fields the request left unset.
func (d *Dispatcher) sandboxConfig() *types.SandboxConfig {
	sc := d.req.Sandbox
	if sc == nil {
		return nil
	}
	if sc.Image == "" && d.cfg.SandboxImage != "" {
		sc.Image = d.cfg.SandboxImage
	}
	if sc.Workdir == "" && d.cfg.SandboxWorkdir != "" {
		sc.Workdir = d.cfg.SandboxWorkdir
	}
	if sc.TimeoutMs == 0 && d.cfg.SandboxTimeoutMs != 0 {
		sc.TimeoutMs = d.cfg.SandboxTimeoutMs
	}
	return sc
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func sessionIDFromContext(req *types.RunRequest) (string, bool) {
	if req.Context == nil {
		return "", false
	}
	if v, ok := req.Context["session_id"].(string); ok && v != "" {
		return v, true
	}
	return "", false
}

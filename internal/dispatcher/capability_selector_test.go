package dispatcher

import (
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/plugins"
	"github.com/good-fish-man/agent-runtime/internal/types"
)

func TestSelectBuiltinToolsByIntent(t *testing.T) {
	tools := selectBuiltinTools("帮我修改 main.go 并运行测试", false)
	for _, want := range []string{"Read", "Edit", "Write", "Bash"} {
		if !contains(tools, want) {
			t.Fatalf("tools %v missing %s", tools, want)
		}
	}
	if contains(tools, "WebSearch") {
		t.Fatalf("unrelated web tool selected: %v", tools)
	}
}

func TestSelectRelevantSkills(t *testing.T) {
	skills := []types.Skill{
		{ID: "pptx", Name: "pptx", Description: "创建幻灯片和演示文稿"},
		{ID: "csv", Name: "csv", Description: "分析 CSV 数据并生成图表"},
		{ID: "s3", Name: "s3", Description: "上传文件到对象存储"},
	}
	selected := selectRelevantSkills(skills, "请分析这个 CSV 并生成图表", 2)
	if len(selected) == 0 || selected[0].ID != "csv" {
		t.Fatalf("selected = %v, want csv first", skillNames(selected))
	}
}

func TestUnrelatedSkillsAreNotSelected(t *testing.T) {
	skills := []types.Skill{{ID: "pptx", Name: "pptx", Description: "Create presentations"}}
	if selected := selectRelevantSkills(skills, "你好，介绍一下自己", 3); len(selected) != 0 {
		t.Fatalf("unexpected skills: %v", skillNames(selected))
	}
}

func TestEnglishKeywordsUseWholeTokens(t *testing.T) {
	tools := selectBuiltinTools("explain the agent-runtime architecture", false)
	if contains(tools, "Bash") {
		t.Fatalf("run inside runtime must not trigger Bash: %v", tools)
	}
}

func TestShippedSkillRouting(t *testing.T) {
	skills := plugins.DiscoverSkillsFromDir("../../skills")
	if len(skills) < 6 {
		t.Fatalf("discovered %d shipped skills, want at least 6", len(skills))
	}
	tests := []struct {
		prompt string
		want   string
	}{
		{"帮我创建一个产品介绍 PPT", "pptx"},
		{"分析这个 CSV 并生成图表", "csv-data-analysis"},
		{"打开网站并截一张图", "agent-browser"},
		{"把这个 PDF 转换成 Markdown", "markitdown"},
		{"把文件上传到 S3", "s3-upload"},
		{"帮我创建一个新的技能", "skill-creator"},
	}
	for _, tt := range tests {
		selected := selectRelevantSkills(skills, tt.prompt, 3)
		if len(selected) == 0 || selected[0].ID != tt.want {
			t.Errorf("prompt %q selected %v, want %s first", tt.prompt, skillNames(selected), tt.want)
		}
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

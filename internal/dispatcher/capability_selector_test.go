package dispatcher

import (
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/capability"
	"github.com/good-fish-man/agent-runtime/internal/plugins"
	"github.com/good-fish-man/agent-runtime/internal/types"
)

func TestSelectBuiltinCapabilitiesByIntent(t *testing.T) {
	capabilities := selectBuiltinCapabilities("帮我修改 main.go 并运行测试", false)
	for _, want := range []string{capability.FilesystemRead, capability.FilesystemEdit, capability.FilesystemWrite, capability.SystemShell} {
		if !contains(capabilities, want) {
			t.Fatalf("capabilities %v missing %s", capabilities, want)
		}
	}
	if contains(capabilities, capability.InternetSearch) {
		t.Fatalf("unrelated internet capability selected: %v", capabilities)
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
	capabilities := selectBuiltinCapabilities("explain the agent-runtime architecture", false)
	if contains(capabilities, capability.SystemShell) {
		t.Fatalf("run inside runtime must not trigger system.shell: %v", capabilities)
	}
}

func TestSelectBuiltinToolsForImplicitWebResearch(t *testing.T) {
	tests := []string{
		"OpenAI 现任 CEO 是谁？",
		"Go 当前稳定版本是多少？",
		"推荐一款今年适合本地运行大模型的笔记本",
		"请核实这个说法并给出官方来源",
		"What is the current exchange rate between USD and AED?",
		"帮我查询一下今天有哪些新闻资讯",
	}
	for _, prompt := range tests {
		selected := selectBuiltinCapabilities(prompt, false)
		for _, wanted := range []string{capability.InternetSearch, capability.InternetFetch, capability.BrowserSearch, capability.BrowserNavigate, capability.BrowserRead, capability.BrowserObserve} {
			if !contains(selected, wanted) {
				t.Errorf("prompt %q did not enable %s: %v", prompt, wanted, selected)
			}
		}
		if contains(selected, capability.BrowserClose) {
			t.Errorf("prompt %q should not enable browser.close by default: %v", prompt, selected)
		}
	}
}

func TestBrowserCloseRequiresExplicitCloseIntent(t *testing.T) {
	openSelected := selectBuiltinCapabilities("打开 YouTube 搜索 AI Agent 教程", false)
	if contains(openSelected, capability.BrowserClose) {
		t.Fatalf("browser.close selected for normal browsing intent: %v", openSelected)
	}
	closeSelected := selectBuiltinCapabilities("关闭浏览器", false)
	if !contains(closeSelected, capability.BrowserClose) {
		t.Fatalf("browser.close not selected for explicit close intent: %v", closeSelected)
	}
}

func TestLocalProjectRequestDoesNotEnableWebTools(t *testing.T) {
	capabilities := selectBuiltinCapabilities("帮我优化当前项目的错误处理", false)
	if contains(capabilities, capability.InternetSearch) || contains(capabilities, capability.InternetFetch) {
		t.Fatalf("local project request enabled internet capabilities: %v", capabilities)
	}
}

func TestLocalDeviceIntentsSelectDedicatedTools(t *testing.T) {
	fileCapabilities := selectBuiltinCapabilities("帮我查找电脑里的 PDF 文件", false)
	if !contains(fileCapabilities, capability.DesktopAction) {
		t.Fatalf("local file capability not selected: %v", fileCapabilities)
	}
	appCapabilities := selectBuiltinCapabilities("帮我打开 Spotify 软件", false)
	if !contains(appCapabilities, capability.DesktopAction) {
		t.Fatalf("application capability not selected: %v", appCapabilities)
	}
	websiteCapabilities := selectBuiltinCapabilities("帮我打开 Acme Portal", false)
	if !contains(websiteCapabilities, capability.DesktopAction) || !contains(websiteCapabilities, capability.BrowserOpen) || !contains(websiteCapabilities, capability.BrowserNavigate) || !contains(websiteCapabilities, capability.BrowserObserve) {
		t.Fatalf("generic open-target capabilities not selected: %v", websiteCapabilities)
	}
	if contains(websiteCapabilities, capability.BrowserClose) {
		t.Fatalf("generic open-target intent should not select browser.close: %v", websiteCapabilities)
	}
	resultCapabilities := selectBuiltinCapabilities("device observation: file search completed", false)
	if contains(resultCapabilities, capability.DesktopAction) {
		t.Fatalf("desktop result selected another action: %v", resultCapabilities)
	}
}

func TestTravelPlanningEnablesResearchLoopTools(t *testing.T) {
	capabilities := selectBuiltinCapabilities("下个月去北海道旅行五天", false)
	for _, want := range []string{capability.InternetSearch, capability.InternetFetch, capability.PlanningTodo, capability.InteractionAsk} {
		if !contains(capabilities, want) {
			t.Fatalf("travel planning capabilities %v missing %s", capabilities, want)
		}
	}
}

func TestAuthenticatedPageEnablesBrowserSessionTools(t *testing.T) {
	selected := selectBuiltinCapabilities("这个网页需要扫码登录，登录后帮我获取账单", false)
	for _, want := range []string{capability.BrowserNavigate, capability.BrowserLogin, capability.BrowserRead, capability.BrowserObserve, capability.BrowserClose} {
		if !contains(selected, want) {
			t.Fatalf("authenticated browser tools %v missing %s", selected, want)
		}
	}
}

func TestBrowserExtendedIntentsSelectRuntimeCapabilities(t *testing.T) {
	downloadSelected := selectBuiltinCapabilities("打开页面以后下载这个 PDF 文件", false)
	for _, want := range []string{capability.BrowserOpen, capability.BrowserObserve, capability.BrowserAction, capability.BrowserDownload} {
		if !contains(downloadSelected, want) {
			t.Fatalf("download browser capabilities %v missing %s", downloadSelected, want)
		}
	}
	screenshotSelected := selectBuiltinCapabilities("给当前网页截一张图", false)
	for _, want := range []string{capability.BrowserObserve, capability.BrowserAction, capability.BrowserScreenshot} {
		if !contains(screenshotSelected, want) {
			t.Fatalf("screenshot browser capabilities %v missing %s", screenshotSelected, want)
		}
	}
}

func TestScheduledMonitoringIntentEnablesCreationTool(t *testing.T) {
	selected := selectBuiltinCapabilities("每五分钟检查一次演唱会余票，有票提醒我", false)
	if !contains(selected, capability.AutomationSchedule) {
		t.Fatalf("scheduled task capability not selected: %v", selected)
	}
}

func TestBackgroundMonitorCapabilitiesAreReadOnly(t *testing.T) {
	selected := readOnlyMonitorCapabilities([]string{capability.SystemShell, capability.FilesystemWrite, capability.InternetSearch, capability.BrowserRead})
	if contains(selected, capability.SystemShell) || contains(selected, capability.FilesystemWrite) {
		t.Fatalf("unsafe background capabilities retained: %v", selected)
	}
	for _, want := range []string{capability.InternetSearch, capability.InternetFetch, capability.BrowserRead} {
		if !contains(selected, want) {
			t.Fatalf("background tools %v missing %s", selected, want)
		}
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

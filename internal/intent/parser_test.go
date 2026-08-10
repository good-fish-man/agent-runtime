package intent

import "testing"

func TestParseDirectBrowserControl(t *testing.T) {
	parsed := Parse(Request{Text: "Open youtub home page and play the second vido"})
	if !parsed.HasSignal(SignalDirectBrowserControl) || !parsed.HasDomain(DomainBrowser) {
		t.Fatalf("unexpected intent: %+v", parsed)
	}
	if parsed.Mode != ModeExecute || parsed.HasSignal(SignalWebAccess) {
		t.Fatalf("direct browser command was parsed as research: %+v", parsed)
	}
}

func TestParseCurrentNewsResearch(t *testing.T) {
	parsed := Parse(Request{Text: "帮我查询今天的科技新闻"})
	if !parsed.HasSignal(SignalWebAccess) || !parsed.HasDomain(DomainResearch) || parsed.Mode != ModeResearch {
		t.Fatalf("unexpected research intent: %+v", parsed)
	}
}

func TestParseEnglishInvestigationResearch(t *testing.T) {
	parsed := Parse(Request{Text: "Investigate Model Context Protocol architecture and cite reliable official and independent sources."})
	if !parsed.HasSignal(SignalWebAccess) || parsed.Mode != ModeResearch {
		t.Fatalf("English investigation was not parsed as research: %+v", parsed)
	}
}

func TestParseWorkspaceWrite(t *testing.T) {
	parsed := Parse(Request{Text: "帮我修改 main.go 并运行测试"})
	for _, signal := range []Signal{SignalWorkspaceRead, SignalWorkspaceWrite, SignalCommand} {
		if !parsed.HasSignal(signal) {
			t.Fatalf("workspace intent missing %s: %+v", signal, parsed)
		}
	}
}

func TestParseExplicitDesktopApplication(t *testing.T) {
	parsed := Parse(Request{Text: "帮我打开 Spotify 软件"})
	if !parsed.HasSignal(SignalExplicitDesktop) || !parsed.HasDomain(DomainDesktop) || parsed.HasSignal(SignalDirectBrowserControl) {
		t.Fatalf("unexpected desktop intent: %+v", parsed)
	}
}

func TestParseActiveBrowserFollowUp(t *testing.T) {
	parsed := Parse(Request{Text: "play the second one", ActiveBrowserSession: true})
	if !parsed.HasSignal(SignalDirectBrowserControl) {
		t.Fatalf("active browser follow-up was not recognized: %+v", parsed)
	}
}

func TestParseCurrentMediaControlWithCommonQuitTypo(t *testing.T) {
	for _, command := range []string{"Quite current video", "Quit current video", "Pause this video", "停止播放当前视频"} {
		parsed := Parse(Request{Text: command, ActiveBrowserSession: true})
		if !parsed.HasSignal(SignalDirectBrowserControl) || !parsed.HasDomain(DomainBrowser) || parsed.Mode != ModeExecute {
			t.Fatalf("current media command %q was not routed to the browser: %+v", command, parsed)
		}
		if parsed.HasSignal(SignalBrowserClose) {
			t.Fatalf("current media command %q must not close the whole browser session: %+v", command, parsed)
		}
	}
}

func TestParseContextualMediaTitle(t *testing.T) {
	parsed := Parse(Request{
		Text:                 "Adele Hello",
		ActiveBrowserSession: true,
		PreviousUserMessages: []string{"Open YouTube and play a music video"},
	})
	if !parsed.HasSignal(SignalDirectBrowserControl) || !parsed.HasSignal(SignalContextualMediaTitle) || parsed.Mode != ModeExecute {
		t.Fatalf("contextual media title was not recognized: %+v", parsed)
	}
}

func TestParseContextualMediaTitleAcrossPreviousTitle(t *testing.T) {
	parsed := Parse(Request{
		Text:                 "Michael Jackson Thriller",
		ActiveBrowserSession: true,
		PreviousUserMessages: []string{"Open YouTube and play a music video", "Adele Hello"},
	})
	if !parsed.HasSignal(SignalContextualMediaTitle) {
		t.Fatalf("successive media title was not recognized: %+v", parsed)
	}
}

func TestParseMediaContextDoesNotCaptureOrdinaryConversation(t *testing.T) {
	parsed := Parse(Request{
		Text:                 "Tell me a joke",
		ActiveBrowserSession: true,
		PreviousUserMessages: []string{"Open YouTube and play a music video"},
	})
	if parsed.HasSignal(SignalDirectBrowserControl) || parsed.HasSignal(SignalContextualMediaTitle) {
		t.Fatalf("ordinary conversation inherited browser media context: %+v", parsed)
	}
}

func TestParseActiveBrowserObservationQuestion(t *testing.T) {
	parsed := Parse(Request{
		Text:                 "What is the title?",
		ActiveBrowserSession: true,
		PreviousUserMessages: []string{"Open YouTube and play a music video"},
	})
	if !parsed.HasSignal(SignalDirectBrowserControl) || parsed.HasSignal(SignalContextualMediaTitle) {
		t.Fatalf("browser observation question was not routed to the current page: %+v", parsed)
	}
}

func TestParseBareTitleNeedsMediaContext(t *testing.T) {
	parsed := Parse(Request{Text: "Adele Hello", ActiveBrowserSession: true})
	if parsed.HasSignal(SignalDirectBrowserControl) || parsed.HasSignal(SignalContextualMediaTitle) {
		t.Fatalf("bare title escaped conversation without media context: %+v", parsed)
	}
}

func TestParseContextualMediaTitleDoesNotCaptureQuestion(t *testing.T) {
	parsed := Parse(Request{
		Text:                 "What is this?",
		ActiveBrowserSession: true,
		PreviousUserMessages: []string{"Open YouTube and play a music video"},
	})
	if parsed.HasSignal(SignalContextualMediaTitle) {
		t.Fatalf("ordinary question was treated as a media title: %+v", parsed)
	}
}

func TestParseInformationalRequestDoesNotInheritBrowserMediaContext(t *testing.T) {
	parsed := Parse(Request{
		Text:                 "想切换驾照，我应该怎么做",
		ActiveBrowserSession: true,
		PreviousUserMessages: []string{"Open YouTube and play a music video"},
	})
	if parsed.HasSignal(SignalDirectBrowserControl) || parsed.HasSignal(SignalContextualMediaTitle) {
		t.Fatalf("informational request inherited browser control: %+v", parsed)
	}
	if !parsed.HasSignal(SignalWebAccess) || parsed.Mode != ModeResearch || !parsed.HasDomain(DomainResearch) {
		t.Fatalf("official procedure was not routed to research: %+v", parsed)
	}
}

func TestParseExplicitResearchSearchDoesNotReuseActiveBrowser(t *testing.T) {
	parsed := Parse(Request{
		Text:                 "搜索驾照换证流程",
		ActiveBrowserSession: true,
		PreviousUserMessages: []string{"Open YouTube home page"},
	})
	if parsed.HasSignal(SignalDirectBrowserControl) || !parsed.HasSignal(SignalWebAccess) || parsed.Mode != ModeResearch {
		t.Fatalf("research search was routed to the active browser: %+v", parsed)
	}
}

func TestParsePoliteExplicitBrowserCommand(t *testing.T) {
	parsed := Parse(Request{Text: "Could you open YouTube?"})
	if !parsed.HasSignal(SignalOpenTarget) || parsed.HasSignal(SignalWebAccess) || parsed.Mode != ModeExecute || !parsed.HasDomain(DomainBrowser) {
		t.Fatalf("polite browser command was treated as an informational question: %+v", parsed)
	}
}

func TestParseBrowserHowToQuestionDoesNotExecuteDeviceAction(t *testing.T) {
	for _, prompt := range []string{"如何打开 Chrome 浏览器？", "这个网页应该怎么登录？", "在哪里下载这个文件？", "怎么关闭浏览器？"} {
		parsed := Parse(Request{Text: prompt, ActiveBrowserSession: true})
		for _, signal := range []Signal{SignalDirectBrowserControl, SignalBrowserAuthentication, SignalBrowserDownload, SignalBrowserClose} {
			if parsed.HasSignal(signal) {
				t.Fatalf("how-to question %q selected device signal %s: %+v", prompt, signal, parsed)
			}
		}
	}
}

func TestParsePoliteBrowserLoginStillExecutes(t *testing.T) {
	parsed := Parse(Request{Text: "可以帮我登录这个网页吗？"})
	if !parsed.HasSignal(SignalBrowserAuthentication) || parsed.Mode == ModeResearch {
		t.Fatalf("explicit login request was treated as advice: %+v", parsed)
	}
}

func TestParseContextualMediaTitleStopsAfterContextSwitch(t *testing.T) {
	parsed := Parse(Request{
		Text:                 "Adele Hello",
		ActiveBrowserSession: true,
		PreviousUserMessages: []string{"Open YouTube and play a music video", "Find today's technology news"},
	})
	if parsed.HasSignal(SignalContextualMediaTitle) {
		t.Fatalf("media context leaked across a research request: %+v", parsed)
	}
}

func TestParseUsesWholeEnglishTokens(t *testing.T) {
	parsed := Parse(Request{Text: "explain the agent-runtime architecture"})
	if parsed.HasSignal(SignalCommand) {
		t.Fatalf("run inside runtime triggered command intent: %+v", parsed)
	}
}

func TestParseResearchRefinementFromConversation(t *testing.T) {
	parsed := Parse(Request{
		Text:                 "Tokyo",
		PreviousUserMessages: []string{"Find today's technology news"},
	})
	if !parsed.HasSignal(SignalContextualResearch) || !parsed.HasSignal(SignalWebAccess) {
		t.Fatalf("research refinement was not recognized: %+v", parsed)
	}
}

func TestParseDirectBrowserCommandOverridesPreviousResearch(t *testing.T) {
	parsed := Parse(Request{
		Text:                 "Open YouTube home page",
		PreviousUserMessages: []string{"Find today's technology news"},
	})
	if !parsed.HasSignal(SignalDirectBrowserControl) || parsed.HasSignal(SignalContextualResearch) {
		t.Fatalf("direct browser command inherited research: %+v", parsed)
	}
}

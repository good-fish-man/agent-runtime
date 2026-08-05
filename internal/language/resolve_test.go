package language

import "testing"

func TestResolveUsesFrontendLocaleByDefault(t *testing.T) {
	if got := Resolve("zh-CN", "hello"); got.Name != "Chinese" || got.Explicit {
		t.Fatalf("selection = %+v", got)
	}
	if got := Resolve("en-US", "请介绍一下自己"); got.Name != "English" || got.Explicit {
		t.Fatalf("selection = %+v", got)
	}
}

func TestResolveExplicitInstructionOverridesFrontendLocale(t *testing.T) {
	if got := Resolve("en-US", "请用中文回答"); got.Name != "Chinese" || !got.Explicit {
		t.Fatalf("selection = %+v", got)
	}
	if got := Resolve("zh-CN", "Please answer in English"); got.Name != "English" || !got.Explicit {
		t.Fatalf("selection = %+v", got)
	}
}

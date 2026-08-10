package tools

import "github.com/cloudwego/eino/components/tool"

// AllToolsWithBasePath instantiates every built-in tool bound to basePath and
// returns them as eino tool.BaseTool values ready for compose.ToolsNodeConfig.
// basePath is used as the working directory / base path for tools that operate
// on the filesystem or shell (Glob, Grep, Read, Edit, Write, Bash, Task*).
func AllToolsWithBasePath(basePath string) []tool.BaseTool {
	return ToolsByNamesWithBasePath(basePath, ToolNames())
}

// ToolsByNamesWithBasePath instantiates only the selected built-in tools.
func ToolsByNamesWithBasePath(basePath string, names []string) []tool.BaseTool {
	if basePath == "" {
		basePath = "."
	}
	builders := map[string]func() ToolInterface{
		"Glob":                    func() ToolInterface { return NewGlobTool(basePath) },
		"Grep":                    func() ToolInterface { return NewGrepTool(basePath) },
		"Read":                    func() ToolInterface { return NewFileReadTool(basePath) },
		"Edit":                    func() ToolInterface { return NewFileEditTool(basePath) },
		"Write":                   func() ToolInterface { return NewFileWriteTool(basePath) },
		"Bash":                    func() ToolInterface { return NewBashTool(basePath) },
		"Sleep":                   func() ToolInterface { return NewSleepTool() },
		"WebFetch":                func() ToolInterface { return NewWebFetchTool() },
		"WebSearch":               func() ToolInterface { return NewWebSearchTool() },
		BrowserLoginToolName:      func() ToolInterface { return NewBrowserLoginTool() },
		BrowserSearchToolName:     func() ToolInterface { return NewBrowserSearchTool() },
		BrowserTaskToolName:       func() ToolInterface { return NewBrowserTaskTool() },
		BrowserOpenToolName:       func() ToolInterface { return NewBrowserOpenTool() },
		BrowserNavigateToolName:   func() ToolInterface { return NewBrowserNavigateTool() },
		BrowserObserveToolName:    func() ToolInterface { return NewBrowserObserveTool() },
		BrowserActionToolName:     func() ToolInterface { return NewBrowserActionTool() },
		BrowserAutomationToolName: func() ToolInterface { return NewBrowserAutomationTool() },
		BrowserReadToolName:       func() ToolInterface { return NewBrowserReadTool() },
		BrowserCloseToolName:      func() ToolInterface { return NewBrowserCloseTool() },
		DesktopActionToolName:     func() ToolInterface { return NewDesktopActionTool() },
		"TaskCreate":              func() ToolInterface { return NewTaskCreateTool(basePath) },
		"TaskGet":                 func() ToolInterface { return NewTaskGetTool(basePath) },
		"TaskList":                func() ToolInterface { return NewTaskListTool(basePath) },
		"TaskUpdate":              func() ToolInterface { return NewTaskUpdateTool(basePath) },
		"TodoWrite":               func() ToolInterface { return NewTodoWriteTool(basePath) },
		"EnterPlanMode":           func() ToolInterface { return NewEnterPlanModeTool() },
		"ExitPlanMode":            func() ToolInterface { return NewExitPlanModeTool() },
		"AskUserQuestion":         func() ToolInterface { return NewAskUserQuestionTool() },
	}
	out := make([]tool.BaseTool, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		if build, ok := builders[name]; ok {
			out = append(out, build())
			seen[name] = struct{}{}
		}
	}
	return out
}

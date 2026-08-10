package capability

const (
	InternetSearch     = "internet.search"
	InternetFetch      = "internet.fetch"
	GitHubSearch       = "github.search"
	WeatherCurrent     = "weather.current"
	MapsRoute          = "maps.route"
	FilesystemList     = "filesystem.list"
	FilesystemSearch   = "filesystem.search"
	FilesystemRead     = "filesystem.read"
	FilesystemEdit     = "filesystem.edit"
	FilesystemWrite    = "filesystem.write"
	PythonExecute      = "python.execute"
	SystemShell        = "system.shell"
	SystemWait         = "system.wait"
	BrowserSearch      = "browser.search"
	BrowserTask        = "browser.task"
	BrowserOpen        = "browser.open"
	BrowserNavigate    = "browser.navigate"
	BrowserLogin       = "browser.login"
	BrowserRead        = "browser.read"
	BrowserObserve     = "browser.observe"
	BrowserAction      = "browser.action"
	BrowserWait        = "browser.wait"
	BrowserDownload    = "browser.download"
	BrowserScreenshot  = "browser.screenshot"
	BrowserAutomation  = "browser.automation"
	BrowserClose       = "browser.close"
	DesktopAction      = "desktop.action"
	PlanningTodo       = "planning.todo"
	PlanningEnter      = "planning.enter"
	PlanningExit       = "planning.exit"
	TaskCreate         = "task.create"
	TaskGet            = "task.get"
	TaskList           = "task.list"
	TaskUpdate         = "task.update"
	InteractionAsk     = "interaction.ask"
	AutomationSchedule = "automation.schedule"
	ImageGenerate      = "media.image.generate"
	VideoGenerate      = "media.video.generate"
)

func init() {
	available := []struct {
		definition Definition
		provider   string
	}{
		{Definition{ID: InternetSearch, Description: "Search public web pages", Input: map[string]string{"query": "string", "count": "integer"}, Output: "SearchResult[]", ReadOnly: true}, "WebSearch"},
		{Definition{ID: InternetFetch, Description: "Fetch and extract public webpage content", Input: map[string]string{"url": "string"}, Output: "Markdown", ReadOnly: true}, "WebFetch"},
		{Definition{ID: FilesystemList, Description: "List files matching a path pattern", Input: map[string]string{"pattern": "string"}, Output: "FilePath[]", ReadOnly: true}, "Glob"},
		{Definition{ID: FilesystemSearch, Description: "Search text in authorized project files", Input: map[string]string{"pattern": "string"}, Output: "TextMatch[]", ReadOnly: true}, "Grep"},
		{Definition{ID: FilesystemRead, Description: "Read an authorized local project file", Input: map[string]string{"file_path": "string"}, Output: "string", ReadOnly: true}, "Read"},
		{Definition{ID: FilesystemEdit, Description: "Apply an exact edit to an authorized project file", Input: map[string]string{"file_path": "string"}, Output: "EditResult", Risk: "medium"}, "Edit"},
		{Definition{ID: FilesystemWrite, Description: "Write an authorized project file", Input: map[string]string{"file_path": "string", "content": "string"}, Output: "WriteResult", Risk: "medium"}, "Write"},
		{Definition{ID: SystemShell, Description: "Execute a shell command inside the authorized project workspace", Input: map[string]string{"command": "string"}, Output: "CommandResult", Risk: "high"}, "Bash"},
		{Definition{ID: SystemWait, Description: "Wait for a bounded duration", Input: map[string]string{"seconds": "number"}, Output: "WaitResult", ReadOnly: true}, "Sleep"},
		{Definition{ID: BrowserSearch, Description: "Search public pages with a real browser", Input: map[string]string{"query": "string"}, Output: "BrowserSearchResult", ReadOnly: true}, "BrowserSearch"},
		{Definition{ID: BrowserTask, Description: "Execute a reversible browser task through the local Browser System and return structured observations", Input: map[string]string{"goal": "string", "session_id": "string", "target": "string", "query": "string"}, Output: "BrowserTaskResult", Risk: "medium"}, "BrowserTask"},
		{Definition{ID: BrowserOpen, Description: "Open a website by URL or discoverable name in a controllable browser session", Input: map[string]string{"target": "string"}, Output: "BrowserSession", Risk: "medium"}, "BrowserOpen"},
		{Definition{ID: BrowserNavigate, Description: "Navigate an existing browser session to an exact URL", Input: map[string]string{"session_id": "string", "url": "string"}, Output: "BrowserObservation", Risk: "medium"}, "BrowserNavigate"},
		{Definition{ID: BrowserLogin, Description: "Open a user-visible browser for interactive authentication", Input: map[string]string{"url": "string"}, Output: "BrowserSession", Risk: "medium"}, "BrowserLogin"},
		{Definition{ID: BrowserRead, Description: "Read a page from an authorized browser session", Input: map[string]string{"session_id": "string", "url": "string"}, Output: "Markdown", ReadOnly: true}, "BrowserRead"},
		{Definition{ID: BrowserObserve, Description: "Observe the current browser session state after an action", Input: map[string]string{"session_id": "string"}, Output: "BrowserObservation", ReadOnly: true}, "BrowserObserve"},
		{Definition{ID: BrowserAction, Description: "Perform a reversible browser navigation action", Input: map[string]string{"session_id": "string", "action": "string"}, Output: "BrowserSnapshot", Risk: "medium"}, "BrowserAction"},
		{Definition{ID: BrowserWait, Description: "Wait briefly for the current browser session to settle", Input: map[string]string{"session_id": "string", "milliseconds": "integer"}, Output: "BrowserObservation", ReadOnly: true}, "BrowserAction"},
		{Definition{ID: BrowserScreenshot, Description: "Capture a screenshot of the current browser page", Input: map[string]string{"session_id": "string"}, Output: "BrowserScreenshot", ReadOnly: true}, "BrowserAction"},
		{Definition{ID: BrowserDownload, Description: "Download a user-requested file by clicking a semantic browser ref", Input: map[string]string{"session_id": "string", "ref": "string", "filename": "string"}, Output: "BrowserDownload", Risk: "medium"}, "BrowserAction"},
		{Definition{ID: BrowserAutomation, Description: "Manage safe event-driven browser watch rules", Input: map[string]string{"operation": "string", "session_id": "string"}, Output: "BrowserAutomationRule", Risk: "medium"}, "BrowserAutomation"},
		{Definition{ID: BrowserClose, Description: "Close a browser session", Input: map[string]string{"session_id": "string"}, Output: "CloseResult", Risk: "medium"}, "BrowserClose"},
		{Definition{ID: DesktopAction, Description: "Open, observe, and control an installed application through an authorized desktop session", Input: map[string]string{"action": "string", "session_id": "string"}, Output: "DesktopActionRequest", Risk: "medium"}, "DesktopAction"},
		{Definition{ID: PlanningTodo, Description: "Track the current execution plan", Input: map[string]string{"todos": "Todo[]"}, Output: "Todo[]"}, "TodoWrite"},
		{Definition{ID: PlanningEnter, Description: "Enter planning mode", Output: "PlanState"}, "EnterPlanMode"},
		{Definition{ID: PlanningExit, Description: "Exit planning mode", Output: "PlanState"}, "ExitPlanMode"},
		{Definition{ID: TaskCreate, Description: "Create a local task", Input: map[string]string{"subject": "string"}, Output: "Task", Risk: "medium"}, "TaskCreate"},
		{Definition{ID: TaskGet, Description: "Read a local task", Input: map[string]string{"id": "string"}, Output: "Task", ReadOnly: true}, "TaskGet"},
		{Definition{ID: TaskList, Description: "List local tasks", Output: "Task[]", ReadOnly: true}, "TaskList"},
		{Definition{ID: TaskUpdate, Description: "Update a local task", Input: map[string]string{"id": "string"}, Output: "Task", Risk: "medium"}, "TaskUpdate"},
		{Definition{ID: InteractionAsk, Description: "Ask the user for a blocking preference or decision", Input: map[string]string{"questions": "Question[]"}, Output: "UserInputRequest", ReadOnly: true}, "AskUserQuestion"},
	}
	for _, item := range available {
		mustRegister(item.definition, item.provider)
	}

	unavailable := []Definition{
		{ID: GitHubSearch, Description: "Search GitHub repositories", Input: map[string]string{"query": "string"}, Output: "GitHubRepository[]", ReadOnly: true, Reason: "No GitHub provider configured"},
		{ID: WeatherCurrent, Description: "Get current weather", Input: map[string]string{"location": "string"}, Output: "CurrentWeather", ReadOnly: true, Reason: "No weather provider configured"},
		{ID: MapsRoute, Description: "Calculate a route", Input: map[string]string{"origin": "string", "destination": "string"}, Output: "Route", ReadOnly: true, Reason: "No maps provider configured"},
		{ID: PythonExecute, Description: "Execute Python code in an isolated sandbox", Input: map[string]string{"code": "string"}, Output: "PythonResult", Risk: "high", Reason: "No isolated Python provider configured"},
		{ID: AutomationSchedule, Description: "Create a persistent scheduled task", Input: map[string]string{"schedule": "string"}, Output: "ScheduledTask", Risk: "medium", Reason: "Requires request-scoped provider"},
		{ID: ImageGenerate, Description: "Generate an image", Input: map[string]string{"prompt": "string"}, Output: "ImageResult", Risk: "medium", Reason: "Requires a request-scoped image model"},
		{ID: VideoGenerate, Description: "Generate a video", Input: map[string]string{"prompt": "string"}, Output: "VideoResult", Risk: "medium", Reason: "Requires a request-scoped video model"},
	}
	for _, definition := range unavailable {
		mustRegister(definition, "")
	}
}

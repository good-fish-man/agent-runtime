package intent

type Domain string

const (
	DomainConversation Domain = "conversation"
	DomainResearch     Domain = "research"
	DomainBrowser      Domain = "browser"
	DomainFile         Domain = "file"
	DomainDesktop      Domain = "desktop"
	DomainAutomation   Domain = "automation"
	DomainPlanning     Domain = "planning"
	DomainTask         Domain = "task"
)

type Mode string

const (
	ModeChat     Mode = "chat"
	ModeRead     Mode = "read"
	ModeWrite    Mode = "write"
	ModeExecute  Mode = "execute"
	ModeResearch Mode = "research"
	ModePlan     Mode = "plan"
)

type Signal string

const (
	SignalUploadedFile          Signal = "uploaded_file"
	SignalWorkspaceRead         Signal = "workspace_read"
	SignalWorkspaceWrite        Signal = "workspace_write"
	SignalCommand               Signal = "command"
	SignalLocalDeviceFile       Signal = "local_device_file"
	SignalOpenTarget            Signal = "open_target"
	SignalExplicitDesktop       Signal = "explicit_desktop"
	SignalDirectBrowserControl  Signal = "direct_browser_control"
	SignalContextualMediaTitle  Signal = "contextual_media_title"
	SignalWebAccess             Signal = "web_access"
	SignalContextualResearch    Signal = "contextual_research"
	SignalBrowserAuthentication Signal = "browser_authentication"
	SignalBrowserDownload       Signal = "browser_download"
	SignalBrowserScreenshot     Signal = "browser_screenshot"
	SignalBrowserClose          Signal = "browser_close"
	SignalPlanning              Signal = "planning"
	SignalTaskManagement        Signal = "task_management"
	SignalWait                  Signal = "wait"
	SignalScheduled             Signal = "scheduled"
)

type Request struct {
	Text                 string
	HasFiles             bool
	ActiveBrowserSession bool
	ActiveDesktopSession bool
	PreviousUserMessages []string
}

type Intent struct {
	Goal       string              `json:"goal"`
	Normalized string              `json:"normalized"`
	Domains    []Domain            `json:"domains"`
	Mode       Mode                `json:"mode"`
	Signals    []Signal            `json:"signals"`
	Entities   map[string][]string `json:"entities,omitempty"`
	Confidence float64             `json:"confidence"`
}

func (i Intent) HasSignal(wanted Signal) bool {
	for _, signal := range i.Signals {
		if signal == wanted {
			return true
		}
	}
	return false
}

func (i Intent) HasDomain(wanted Domain) bool {
	for _, domain := range i.Domains {
		if domain == wanted {
			return true
		}
	}
	return false
}

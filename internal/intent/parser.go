package intent

import (
	"regexp"
	"strings"
)

var (
	latinTokenPattern = regexp.MustCompile(`[a-z0-9][a-z0-9._-]*`)
	urlPattern        = regexp.MustCompile(`https?://[^\s<>"']+`)

	workspaceReadKeywords = []string{
		"代码", "项目", "目录", "文件", "仓库", "源码", "函数", "类", "接口", "报错", "bug", "error", "code", "project", "repository", "repo", "file", "function", "compile",
		".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".java", ".rs", ".vue", ".sql", ".yaml", ".yml", ".json",
	}
	workspaceWriteKeywords = []string{
		"修改", "修复", "实现", "新增", "添加", "删除", "重构", "优化", "完善", "改成", "生成补丁", "write", "edit", "modify", "fix", "implement", "refactor", "optimize", "patch",
	}
	commandKeywords = []string{
		"运行", "执行", "测试", "构建", "编译", "安装", "启动", "命令", "终端", "run", "test", "build", "compile", "install", "start", "command", "terminal", "shell", "npm", "pnpm", "yarn", "go test", "docker",
	}
	localFileSearchKeywords = []string{
		"本地文件", "电脑里的文件", "电脑上的文件", "硬盘里的文件", "下载目录", "文档目录", "桌面上的文件", "查找文件", "搜索文件",
		"local file", "files on my computer", "find a file", "search my files", "downloads folder", "documents folder", "desktop file",
	}
	localFileActionKeywords = []string{"查找", "查询", "搜索", "找一下", "find", "search", "locate"}
	localFilePlaceKeywords  = []string{"本地", "电脑", "硬盘", "下载目录", "文档目录", "桌面", "my computer", "local", "downloads", "documents", "desktop"}
	localFileObjectKeywords = []string{"文件", "文档", "资料", "file", "document"}

	openActionKeywords    = []string{"打开", "启动", "运行", "播放", "open", "launch", "start", "play"}
	desktopObjectKeywords = []string{
		"软件", "应用", "程序", "音乐", "计算器", "播放器", "software", "music", "calculator", "player", "app", "application",
	}
	browserActionKeywords = []string{
		"打开", "播放", "点击", "选择", "输入", "滚动", "返回", "前往", "进入", "搜索", "查找", "等待", "刷新", "关闭",
		"暂停", "停止播放", "退出", "离开",
		"open", "play", "click", "select", "type", "scroll", "navigate", "go to", "search", "find", "wait", "refresh", "close",
		"pause", "stop", "quit", "quite", "exit", "leave",
	}
	browserObjectKeywords = []string{
		"浏览器", "网站", "网页", "页面", "主页", "首页", "标签页", "视频", "链接", "按钮", "搜索框", "网址",
		"browser", "website", "webpage", "page", "home page", "homepage", "tab", "video", "link", "button", "search box", "url",
	}
	browserFollowUpKeywords = []string{
		"它", "这个", "当前", "继续", "第一个", "第二个", "第三个", "上一个", "下一个",
		"it", "this", "current", "continue", "first", "second", "third", "previous", "next",
	}
	browserMediaContextKeywords = []string{
		"视频", "影片", "电影", "歌曲", "音乐", "播放", "观看", "收听",
		"video", "movie", "film", "song", "music", "track", "audio", "play", "watch", "listen",
		"youtube", "youtube music", "bilibili", "b站", "哔哩哔哩", "qq music", "qq音乐", "spotify", "netflix", "网飞", "奈飞",
	}
	browserObservationKeywords = []string{
		"当前页面", "这个页面", "该页面", "页面内容", "页面标题", "当前网页", "这个网页", "当前视频", "这个视频", "现在播放", "正在播放", "总结当前页面", "总结这个页面", "这是什么页面", "这个页面是什么",
		"this page", "current page", "page content", "page title", "this webpage", "current webpage", "this video", "current video", "what is playing", "currently playing", "what is the title", "summarize this page", "what is this page",
	}
	webKeywords = []string{
		"联网", "上网", "搜索网页", "网上查", "查询一下", "查一下", "查证", "核实", "验证信息", "官网", "官方文档", "来源", "出处", "引用", "链接",
		"网站", "网址", "url", "http://", "https://", "web", "search online", "look up", "browse", "website", "source", "sources", "citation", "citations", "official docs", "official documentation", "verify", "investigate",
	}
	webTemporalKeywords = []string{
		"最新", "现任", "目前", "近期", "最近", "今天", "昨日", "昨天", "明天", "下周", "下个月", "本周", "本月", "今年", "截至", "实时", "刚刚", "现在还有", "是否仍然",
		"latest", "current", "currently", "recent", "recently", "today", "yesterday", "this week", "this month", "this year", "as of", "real-time", "still available",
	}
	webMutableFactKeywords = []string{
		"天气", "新闻", "价格", "报价", "汇率", "股价", "市值", "利率", "政策", "法规", "法律", "规定", "标准", "版本", "发布日期", "发布时间", "更新日志",
		"总统", "主席", "首相", "部长", "市长", "ceo", "cto", "负责人", "创始人", "赛程", "比分", "排名", "票房", "航班", "时刻表", "库存", "营业时间",
		"weather", "news", "price", "exchange rate", "stock", "market cap", "interest rate", "policy", "regulation", "law", "standard", "version", "release date", "changelog",
		"president", "prime minister", "minister", "mayor", "founder", "schedule", "score", "ranking", "flight", "opening hours", "availability",
	}
	webOfficialProcedureKeywords = []string{
		"驾照", "驾驶证", "换证", "签证", "护照", "在留卡", "居留", "永住", "入籍", "移民", "税务", "社会保险", "社保", "养老金", "行政手续", "办理流程", "申请条件", "所需材料", "官方手续", "许可证", "执照", "资格认证",
		"driver's license", "driving license", "driving licence", "license conversion", "licence conversion", "visa", "passport", "residence permit", "immigration", "naturalization", "tax", "social security", "pension", "government procedure", "application process", "eligibility requirements", "required documents", "permit", "professional license",
	}
	webRecommendationKeywords = []string{
		"推荐", "值得买吗", "哪个好", "哪家", "哪里吃", "哪里住", "旅游攻略", "购买建议", "选型", "对比一下",
		"recommend", "recommendation", "best", "worth buying", "where to eat", "where to stay", "travel plan", "buying guide", "compare products",
	}
	researchPlanningKeywords = []string{
		"旅行", "旅游", "行程", "出行", "度假", "自驾", "机票", "酒店", "住宿", "攻略", "预算方案", "调研", "深入研究", "多方案比较",
		"travel", "trip", "itinerary", "vacation", "road trip", "flight", "hotel", "accommodation", "research", "deep research", "compare options",
	}
	webExternalKnowledgeKeywords = []string{
		"公司", "产品", "模型", "软件", "框架", "库", "api", "国家", "城市", "政府", "学校", "医院", "比赛", "电影", "餐厅", "酒店", "景点",
		"company", "product", "model", "software", "framework", "library", "country", "city", "government", "school", "hospital", "game", "movie", "restaurant", "hotel",
	}
	webQuestionKeywords = []string{
		"是谁", "是什么", "多少", "怎么样", "如何", "为什么", "有没有", "是否", "哪一个", "哪个", "哪里", "什么时候",
		"who", "what", "how much", "how many", "how", "why", "whether", "which", "where", "when",
	}
	browserAuthenticationKeywords = []string{
		"需要登录", "登录", "登入", "账号密码", "验证码", "扫码登录", "二维码登录", "两步验证", "二次验证", "登录完成", "已完成登录", "authentication_required", "session_id",
		"requires login", "sign in", "log in", "password", "captcha", "scan qr", "qr login", "two-factor", "2fa", "login completed",
	}
	browserCloseKeywords = []string{
		"关闭浏览器", "关闭网页", "关闭页面", "关闭标签页", "关掉浏览器", "关掉网页", "关掉页面",
		"close browser", "close webpage", "close page", "close tab",
	}
	browserDownloadKeywords   = []string{"下载", "保存文件", "下载文件", "保存到本地", "download", "save file"}
	browserScreenshotKeywords = []string{"截图", "截屏", "截一张图", "截个图", "页面截图", "浏览器截图", "screenshot", "screen shot", "capture page"}
	planKeywords              = []string{"计划", "步骤", "待办", "复杂任务", "整体重构", "plan", "todo", "steps", "multi-step"}
	taskKeywords              = []string{"子任务", "并行任务", "任务状态", "parallel task", "subtask", "delegate"}
	waitKeywords              = []string{"等待", "稍后", "sleep", "wait", "delay"}
	scheduledTaskKeywords     = []string{"定时", "周期查询", "持续查询", "到货提醒", "库存提醒", "抢票", "余票", "放票", "抢购", "秒杀", "挂号", "号源", "预约提醒", "每分钟", "每小时", "每天", "schedule", "scheduled", "monitor stock", "ticket availability", "appointment slot", "restock"}
)

func Parse(request Request) Intent {
	goal := strings.TrimSpace(request.Text)
	normalized := strings.ToLower(goal)
	result := Intent{Goal: goal, Normalized: normalized, Mode: ModeChat, Confidence: 0.5}
	seenSignals := make(map[Signal]bool)
	seenDomains := make(map[Domain]bool)
	addSignal := func(signal Signal) {
		if !seenSignals[signal] {
			result.Signals = append(result.Signals, signal)
			seenSignals[signal] = true
		}
	}
	addDomain := func(domain Domain) {
		if !seenDomains[domain] {
			result.Domains = append(result.Domains, domain)
			seenDomains[domain] = true
		}
	}

	if request.HasFiles {
		addSignal(SignalUploadedFile)
		addDomain(DomainFile)
	}
	localDeviceFile := matchesAny(normalized, localFileSearchKeywords) ||
		(matchesAny(normalized, localFileActionKeywords) && matchesAny(normalized, localFilePlaceKeywords) && matchesAny(normalized, localFileObjectKeywords))
	if localDeviceFile {
		addSignal(SignalLocalDeviceFile)
		addDomain(DomainFile)
	}
	if !localDeviceFile && matchesAny(normalized, workspaceReadKeywords) {
		addSignal(SignalWorkspaceRead)
		addDomain(DomainFile)
	}
	if matchesAny(normalized, workspaceWriteKeywords) {
		addSignal(SignalWorkspaceWrite)
		addDomain(DomainFile)
		result.Mode = ModeWrite
	}
	if matchesAny(normalized, commandKeywords) {
		addSignal(SignalCommand)
		addDomain(DomainFile)
		if result.Mode != ModeWrite {
			result.Mode = ModeExecute
		}
	}

	informationalRequest := isInformationalRequest(goal)
	openTarget := matchesAny(normalized, openActionKeywords) && !informationalRequest
	explicitDesktop := openTarget && matchesAny(normalized, desktopObjectKeywords)
	webKnowledgeRequest := matchesAny(normalized, webKeywords) || matchesAny(normalized, webTemporalKeywords) || matchesAny(normalized, webMutableFactKeywords) || matchesAny(normalized, webOfficialProcedureKeywords) || matchesAny(normalized, webRecommendationKeywords) || matchesAny(normalized, researchPlanningKeywords) ||
		(matchesAny(normalized, webQuestionKeywords) && matchesAny(normalized, webExternalKnowledgeKeywords))
	browserAction := matchesAny(normalized, browserActionKeywords)
	directBrowser := browserAction && matchesAny(normalized, browserObjectKeywords) && !informationalRequest
	workspaceTarget := result.HasSignal(SignalWorkspaceRead) || result.HasSignal(SignalWorkspaceWrite) || result.HasSignal(SignalCommand) || localDeviceFile
	activeBrowserContinuation := request.ActiveBrowserSession && browserAction && !informationalRequest && !explicitDesktop && !workspaceTarget &&
		(matchesAny(normalized, browserFollowUpKeywords) || (!webKnowledgeRequest && hasRecentBrowserControl(request.PreviousUserMessages)))
	if activeBrowserContinuation {
		directBrowser = true
	}
	if request.ActiveBrowserSession && matchesAny(normalized, browserObservationKeywords) && hasRecentBrowserControl(request.PreviousUserMessages) {
		directBrowser = true
	}
	contextualMediaTitle := !directBrowser && request.ActiveBrowserSession && isContextualMediaTitle(goal, request.PreviousUserMessages)
	if contextualMediaTitle {
		directBrowser = true
	}
	if openTarget {
		addSignal(SignalOpenTarget)
	}
	if explicitDesktop {
		addSignal(SignalExplicitDesktop)
		addDomain(DomainDesktop)
	}
	if directBrowser {
		addSignal(SignalDirectBrowserControl)
		if contextualMediaTitle {
			addSignal(SignalContextualMediaTitle)
		}
		addDomain(DomainBrowser)
		result.Mode = ModeExecute
	}
	if !informationalRequest && matchesAny(normalized, browserAuthenticationKeywords) {
		addSignal(SignalBrowserAuthentication)
		addDomain(DomainBrowser)
	}
	if !informationalRequest && matchesAny(normalized, browserDownloadKeywords) {
		addSignal(SignalBrowserDownload)
		addDomain(DomainBrowser)
	}
	if !informationalRequest && matchesAny(normalized, browserScreenshotKeywords) {
		addSignal(SignalBrowserScreenshot)
		addDomain(DomainBrowser)
	}
	if !informationalRequest && matchesAny(normalized, browserCloseKeywords) {
		addSignal(SignalBrowserClose)
		addDomain(DomainBrowser)
	}

	webAccess := webKnowledgeRequest
	if !webAccess && !directBrowser && !openTarget && !localDeviceFile && !explicitDesktop && isResearchRefinement(goal, request.PreviousUserMessages) {
		webAccess = true
		addSignal(SignalContextualResearch)
	}
	if webAccess {
		addSignal(SignalWebAccess)
		addDomain(DomainResearch)
		if !directBrowser {
			result.Mode = ModeResearch
		}
	}
	if matchesAny(normalized, planKeywords) || matchesAny(normalized, researchPlanningKeywords) {
		addSignal(SignalPlanning)
		addDomain(DomainPlanning)
		if !webAccess && result.Mode == ModeChat {
			result.Mode = ModePlan
		}
	}
	if matchesAny(normalized, taskKeywords) {
		addSignal(SignalTaskManagement)
		addDomain(DomainTask)
	}
	if matchesAny(normalized, waitKeywords) {
		addSignal(SignalWait)
	}
	if !informationalRequest && matchesAny(normalized, scheduledTaskKeywords) {
		addSignal(SignalScheduled)
		addDomain(DomainAutomation)
		result.Mode = ModeExecute
	}

	if openTarget && !explicitDesktop && !directBrowser && !webAccess {
		addDomain(DomainBrowser)
		addDomain(DomainDesktop)
		result.Mode = ModeExecute
	}
	if len(result.Domains) == 0 {
		addDomain(DomainConversation)
	}
	if result.Mode == ModeChat && (result.HasSignal(SignalUploadedFile) || result.HasSignal(SignalWorkspaceRead) || result.HasSignal(SignalLocalDeviceFile)) {
		result.Mode = ModeRead
	}
	result.Confidence = confidence(result)
	if urls := urlPattern.FindAllString(goal, -1); len(urls) > 0 {
		result.Entities = map[string][]string{"urls": urls}
	}
	return result
}

func confidence(parsed Intent) float64 {
	switch {
	case parsed.HasSignal(SignalDirectBrowserControl), parsed.HasSignal(SignalScheduled):
		return 0.98
	case parsed.HasSignal(SignalBrowserAuthentication), parsed.HasSignal(SignalLocalDeviceFile), parsed.HasSignal(SignalExplicitDesktop):
		return 0.95
	case parsed.HasSignal(SignalWebAccess), parsed.HasSignal(SignalWorkspaceWrite):
		return 0.9
	case parsed.HasSignal(SignalOpenTarget), parsed.HasSignal(SignalWorkspaceRead):
		return 0.8
	default:
		return 0.5
	}
}

func isResearchRefinement(goal string, previous []string) bool {
	goal = strings.TrimSpace(goal)
	if goal == "" || len([]rune(goal)) > 80 || isGreeting(goal) {
		return false
	}
	for index := len(previous) - 1; index >= 0; index-- {
		prior := Parse(Request{Text: previous[index]})
		if prior.HasSignal(SignalWebAccess) {
			return true
		}
	}
	return false
}

// isContextualMediaTitle recognizes a short title as a continuation only when
// the active conversation is already controlling media in the browser. This
// keeps names such as "Adele Hello" out of the general browser route while
// allowing natural follow-ups that omit "search", "open", or "play".
func isContextualMediaTitle(goal string, previous []string) bool {
	goal = strings.TrimSpace(goal)
	if goal == "" || len([]rune(goal)) > 160 || strings.ContainsAny(goal, "\r\n") || urlPattern.MatchString(goal) {
		return false
	}
	normalized := strings.ToLower(goal)
	if matchesAny(normalized, browserActionKeywords) || isContextualMediaReply(normalized) || isGreeting(normalized) || isInformationalRequest(goal) || looksLikeConversationRequest(normalized) ||
		looksLikeDetailedRequest(goal) || matchesAny(normalized, webOfficialProcedureKeywords) {
		return false
	}

	inspected := 0
	for index := len(previous) - 1; index >= 0 && inspected < 4; index-- {
		priorText := strings.TrimSpace(previous[index])
		if priorText == "" || strings.EqualFold(priorText, goal) {
			continue
		}
		inspected++
		prior := Parse(Request{Text: priorText})
		if prior.HasSignal(SignalDirectBrowserControl) {
			return matchesAny(strings.ToLower(priorText), browserMediaContextKeywords)
		}
		if prior.HasSignal(SignalWebAccess) || prior.HasSignal(SignalWorkspaceWrite) || prior.HasSignal(SignalWorkspaceRead) ||
			prior.HasSignal(SignalLocalDeviceFile) || prior.HasSignal(SignalExplicitDesktop) || prior.HasSignal(SignalScheduled) {
			return false
		}
	}
	return false
}

func hasRecentBrowserControl(previous []string) bool {
	inspected := 0
	for index := len(previous) - 1; index >= 0 && inspected < 4; index-- {
		priorText := strings.TrimSpace(previous[index])
		if priorText == "" {
			continue
		}
		inspected++
		prior := Parse(Request{Text: priorText})
		if prior.HasSignal(SignalDirectBrowserControl) {
			return true
		}
		if prior.HasSignal(SignalWebAccess) || prior.HasSignal(SignalWorkspaceWrite) || prior.HasSignal(SignalWorkspaceRead) ||
			prior.HasSignal(SignalLocalDeviceFile) || prior.HasSignal(SignalExplicitDesktop) || prior.HasSignal(SignalScheduled) {
			return false
		}
	}
	return false
}

func isContextualMediaReply(value string) bool {
	value = strings.TrimSpace(value)
	for _, reply := range []string{
		"ok", "okay", "yes", "no", "done", "stop", "cancel", "thanks", "thank you",
		"好的", "好", "是", "不是", "可以", "不用", "完成", "停止", "取消", "谢谢", "謝謝",
	} {
		if value == reply {
			return true
		}
	}
	return false
}

func isInformationalRequest(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || isPoliteBrowserActionRequest(value) {
		return false
	}
	if strings.ContainsAny(value, "?？") {
		return true
	}
	for _, marker := range []string{
		"应该怎么", "應該怎麼", "怎么办", "怎麼辦", "怎么做", "怎麼做", "如何办理", "如何辦理", "如何申请", "如何申請", "需要什么", "需要什麼", "需要哪些", "为什么", "為什麼", "是什么", "是什麼", "能否", "是否可以", "有哪些要求", "有什么要求", "有什麼要求",
		"how do i", "how can i", "how should i", "what should i", "what do i need", "do i need", "can i convert", "can i apply", "what are the requirements", "what is required", "where should i", "why does", "why is",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func isPoliteBrowserActionRequest(value string) bool {
	for _, prefix := range []string{
		"请打开", "請打開", "帮我打开", "幫我打開", "请点击", "請點擊", "帮我点击", "幫我點擊", "请播放", "請播放", "帮我播放", "幫我播放",
		"请登录", "請登錄", "帮我登录", "幫我登錄", "可以帮我登录", "可以幫我登錄", "请下载", "請下載", "帮我下载", "幫我下載", "请截图", "請截圖", "帮我截图", "幫我截圖", "请关闭", "請關閉", "帮我关闭", "幫我關閉",
		"please open ", "please click ", "please play ", "please sign in ", "please log in ", "please download ", "please capture ", "please close ",
		"can you open ", "could you open ", "can you click ", "could you click ", "can you play ", "could you play ", "can you sign in ", "could you sign in ", "can you log in ", "could you log in ", "can you download ", "could you download ", "can you close ", "could you close ",
	} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func looksLikeDetailedRequest(value string) bool {
	runeCount := len([]rune(strings.TrimSpace(value)))
	return runeCount > 80 || (runeCount > 20 && strings.ContainsAny(value, "，,。；;！!"))
}

func looksLikeConversationRequest(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	for _, prefix := range []string{
		"帮我", "幫我", "请", "請", "告诉我", "告訴我", "解释", "解釋", "介绍", "介紹", "写一个", "寫一個", "创建", "創建", "生成", "总结", "總結", "翻译", "翻譯", "推荐", "推薦", "给我", "給我", "我想", "我是", "我有",
		"tell me ", "explain ", "describe ", "help me ", "write ", "create ", "make ", "generate ", "summarize ", "translate ", "recommend ", "give me ", "list ", "i want ", "i am ", "i have ",
	} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func isGreeting(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	for _, greeting := range []string{"hi", "hello", "hey", "你好", "您好", "嗨", "谢谢", "thank you", "thanks"} {
		if value == greeting {
			return true
		}
	}
	return false
}

func matchesAny(text string, keywords []string) bool {
	tokens := tokenSet(text)
	for _, keyword := range keywords {
		keyword = strings.ToLower(keyword)
		if isSimpleLatinToken(keyword) {
			if tokens[keyword] {
				return true
			}
			continue
		}
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func tokenSet(text string) map[string]bool {
	result := make(map[string]bool)
	for _, token := range latinTokenPattern.FindAllString(strings.ToLower(text), -1) {
		result[token] = true
	}
	return result
}

func isSimpleLatinToken(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

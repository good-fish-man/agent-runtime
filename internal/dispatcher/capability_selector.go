package dispatcher

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/good-fish-man/agent-runtime/internal/capability"
	"github.com/good-fish-man/agent-runtime/internal/eino"
	"github.com/good-fish-man/agent-runtime/internal/types"
)

var (
	codeReadKeywords = []string{
		"代码", "项目", "目录", "文件", "仓库", "源码", "函数", "类", "接口", "报错", "bug", "error", "code", "project", "repository", "repo", "file", "function", "compile",
		".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".java", ".rs", ".vue", ".sql", ".yaml", ".yml", ".json",
	}
	codeWriteKeywords = []string{
		"修改", "修复", "实现", "新增", "添加", "删除", "重构", "优化", "完善", "改成", "生成补丁", "write", "edit", "modify", "fix", "implement", "refactor", "optimize", "patch",
	}
	commandKeywords = []string{
		"运行", "执行", "测试", "构建", "编译", "安装", "启动", "命令", "终端", "run", "test", "build", "compile", "install", "start", "command", "terminal", "shell", "npm", "pnpm", "yarn", "go test", "docker",
	}
	localFileSearchKeywords = []string{
		"本地文件", "电脑里的文件", "电脑上的文件", "硬盘里的文件", "下载目录", "文档目录", "桌面上的文件", "查找文件", "搜索文件",
		"local file", "files on my computer", "find a file", "search my files", "downloads folder", "documents folder", "desktop file",
	}
	openApplicationKeywords = []string{
		"打开软件", "启动软件", "打开应用", "启动应用", "运行应用", "播放音乐", "打开音乐", "打开计算器", "打开浏览器",
		"打开网站", "打开网页", "打开网址",
		"open app", "open application", "launch app", "launch application", "start application", "open music", "open calculator", "open website",
	}
	localFileActionKeywords = []string{"查找", "查询", "搜索", "找一下", "find", "search", "locate"}
	localFilePlaceKeywords  = []string{"本地", "电脑", "硬盘", "下载目录", "文档目录", "桌面", "my computer", "local", "downloads", "documents", "desktop"}
	localFileObjectKeywords = []string{"文件", "文档", "资料", "file", "document"}
	openAppActionKeywords   = []string{"打开", "启动", "运行", "播放", "open", "launch", "start", "play"}
	openAppObjectKeywords   = []string{"软件", "应用", "程序", "音乐", "计算器", "浏览器", "网站", "网页", "网址", "music", "calculator", "browser", "website", "app", "application"}
	webKeywords             = []string{
		"联网", "上网", "搜索网页", "网上查", "查询一下", "查一下", "查证", "核实", "验证信息", "官网", "官方文档", "来源", "出处", "引用", "链接",
		"网站", "网址", "url", "http://", "https://", "web", "search", "search online", "look up", "browse", "website", "source", "citation", "official docs", "verify",
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
	webRecommendationKeywords = []string{
		"推荐", "值得买吗", "哪个好", "哪家", "哪里吃", "哪里住", "旅游攻略", "购买建议", "选型", "对比一下",
		"recommend", "recommendation", "best", "worth buying", "where to eat", "where to stay", "travel plan", "buying guide", "compare products",
	}
	researchPlanningKeywords = []string{
		"旅行", "旅游", "行程", "出行", "度假", "自驾", "机票", "酒店", "住宿", "攻略", "预算方案", "调研", "深入研究", "多方案比较",
		"travel", "trip", "itinerary", "vacation", "road trip", "flight", "hotel", "accommodation", "research", "deep research", "compare options",
	}
	browserAuthenticationKeywords = []string{
		"需要登录", "登录网站", "登录网页", "账号密码", "验证码", "扫码登录", "二维码登录", "两步验证", "二次验证", "登录完成", "已完成登录", "authentication_required", "session_id",
		"requires login", "sign in", "log in", "password", "captcha", "scan qr", "qr login", "two-factor", "2fa", "login completed",
	}
	browserCloseKeywords = []string{
		"关闭浏览器", "关闭网页", "关闭页面", "关闭标签页", "关掉浏览器", "关掉网页", "关掉页面",
		"close browser", "close webpage", "close page", "close tab",
	}
	browserDownloadKeywords = []string{
		"下载", "保存文件", "下载文件", "保存到本地", "download", "save file",
	}
	browserScreenshotKeywords = []string{
		"截图", "截屏", "截一张图", "截个图", "页面截图", "浏览器截图", "screenshot", "screen shot", "capture page",
	}
	webExternalKnowledgeKeywords = []string{
		"公司", "产品", "模型", "软件", "框架", "库", "api", "国家", "城市", "政府", "学校", "医院", "比赛", "电影", "餐厅", "酒店", "景点",
		"company", "product", "model", "software", "framework", "library", "country", "city", "government", "school", "hospital", "game", "movie", "restaurant", "hotel",
	}
	webQuestionKeywords = []string{
		"是谁", "是什么", "多少", "怎么样", "如何", "为什么", "有没有", "是否", "哪一个", "哪个", "哪里", "什么时候",
		"who", "what", "how much", "how many", "how", "why", "whether", "which", "where", "when",
	}
	planKeywords = []string{
		"计划", "步骤", "待办", "复杂任务", "整体重构", "plan", "todo", "steps", "multi-step",
	}
	taskKeywords = []string{
		"子任务", "并行任务", "任务状态", "parallel task", "subtask", "delegate",
	}
	waitKeywords          = []string{"等待", "稍后", "sleep", "wait", "delay"}
	scheduledTaskKeywords = []string{"定时", "周期查询", "持续查询", "到货提醒", "库存提醒", "抢票", "余票", "放票", "抢购", "秒杀", "挂号", "号源", "预约提醒", "每分钟", "每小时", "每天", "schedule", "scheduled", "monitor stock", "ticket availability", "appointment slot", "restock"}
	skillCreationKeywords = []string{
		"创建技能", "新增技能", "编写技能", "修改技能", "优化技能", "创建skill", "新增skill", "create skill", "build skill", "improve skill", "update skill",
	}
	latinTokenPattern = regexp.MustCompile(`[a-z0-9][a-z0-9._-]*`)
)

func capabilityText(userPrompt string, msgs []eino.ChatMessage) string {
	parts := []string{userPrompt}
	start := len(msgs) - 3
	if start < 0 {
		start = 0
	}
	for _, msg := range msgs[start:] {
		parts = append(parts, msg.Content)
	}
	return strings.ToLower(strings.Join(parts, "\n"))
}

func selectBuiltinCapabilities(text string, hasFiles bool) []string {
	selected := make([]string, 0, 10)
	seen := map[string]bool{}
	add := func(names ...string) {
		for _, name := range names {
			if !seen[name] {
				selected = append(selected, name)
				seen[name] = true
			}
		}
	}

	// Keep one escape hatch for genuinely ambiguous requests.
	add(capability.InteractionAsk)
	if hasFiles || matchesAny(text, codeReadKeywords) {
		add(capability.FilesystemList, capability.FilesystemSearch, capability.FilesystemRead)
	}
	if matchesAny(text, codeWriteKeywords) {
		add(capability.FilesystemList, capability.FilesystemSearch, capability.FilesystemRead, capability.FilesystemEdit, capability.FilesystemWrite)
	}
	if matchesAny(text, commandKeywords) {
		add(capability.SystemShell)
	}
	openTargetIntent := matchesAny(text, openApplicationKeywords) || matchesAny(text, openAppActionKeywords)
	if matchesAny(text, localFileSearchKeywords) ||
		(matchesAny(text, localFileActionKeywords) && matchesAny(text, localFilePlaceKeywords) && matchesAny(text, localFileObjectKeywords)) ||
		openTargetIntent ||
		(matchesAny(text, openAppActionKeywords) && matchesAny(text, openAppObjectKeywords)) {
		add(capability.DesktopAction)
	}
	// The model decides whether an arbitrary target is a website or an installed app.
	if openTargetIntent {
		add(capability.BrowserOpen, capability.BrowserNavigate, capability.BrowserRead, capability.BrowserObserve, capability.BrowserAction, capability.BrowserWait, capability.BrowserScreenshot)
	}
	if needsWebAccess(text) {
		add(capability.InternetSearch, capability.InternetFetch, capability.BrowserSearch, capability.BrowserNavigate, capability.BrowserRead, capability.BrowserObserve, capability.BrowserAction, capability.BrowserWait, capability.BrowserScreenshot)
	}
	if matchesAny(text, browserAuthenticationKeywords) {
		add(capability.BrowserSearch, capability.BrowserNavigate, capability.BrowserLogin, capability.BrowserRead, capability.BrowserObserve, capability.BrowserAction, capability.BrowserWait, capability.BrowserScreenshot, capability.BrowserClose)
	}
	if matchesAny(text, browserDownloadKeywords) {
		add(capability.BrowserOpen, capability.BrowserObserve, capability.BrowserAction, capability.BrowserDownload)
	}
	if matchesAny(text, browserScreenshotKeywords) {
		add(capability.BrowserObserve, capability.BrowserAction, capability.BrowserScreenshot)
	}
	if matchesAny(text, browserCloseKeywords) {
		add(capability.BrowserClose)
	}
	if matchesAny(text, planKeywords) || matchesAny(text, researchPlanningKeywords) {
		add(capability.PlanningTodo, capability.PlanningEnter, capability.PlanningExit)
	}
	if matchesAny(text, taskKeywords) {
		add(capability.TaskCreate, capability.TaskGet, capability.TaskList, capability.TaskUpdate)
	}
	if matchesAny(text, waitKeywords) {
		add(capability.SystemWait)
	}
	if matchesAny(text, scheduledTaskKeywords) {
		add(capability.AutomationSchedule)
	}
	return selected
}

func needsWebAccess(text string) bool {
	if matchesAny(text, webKeywords) || matchesAny(text, webTemporalKeywords) || matchesAny(text, webMutableFactKeywords) || matchesAny(text, webRecommendationKeywords) || matchesAny(text, researchPlanningKeywords) {
		return true
	}
	return matchesAny(text, webQuestionKeywords) && matchesAny(text, webExternalKnowledgeKeywords)
}

type scoredSkill struct {
	skill types.Skill
	score int
}

func selectRelevantSkills(skills []types.Skill, text string, limit int) []types.Skill {
	if limit <= 0 || len(skills) == 0 {
		return nil
	}
	text = strings.ToLower(text)
	queryTokens := tokenSet(text)
	queryBigrams := cjkBigrams(text)
	scored := make([]scoredSkill, 0, len(skills))
	for _, skill := range skills {
		metadata := strings.ToLower(strings.Join([]string{skill.ID, skill.Name, skill.Description, skill.Trigger}, " "))
		score := 0
		for _, name := range []string{skill.ID, skill.Name} {
			name = strings.ToLower(strings.TrimSpace(name))
			if name != "" && strings.Contains(text, name) {
				score += 12
			}
		}
		for _, trigger := range strings.FieldsFunc(strings.ToLower(skill.Trigger), func(r rune) bool {
			return r == ',' || r == ';' || r == '，' || r == '；'
		}) {
			trigger = strings.TrimSpace(trigger)
			if utf8.RuneCountInString(trigger) >= 2 && strings.Contains(text, trigger) {
				score += 6
			}
		}
		for token := range queryTokens {
			if len(token) >= 3 && strings.Contains(metadata, token) {
				score += 2
			}
		}
		metadataBigrams := cjkBigrams(metadata)
		for gram := range queryBigrams {
			if metadataBigrams[gram] {
				score++
			}
		}
		if score >= 2 {
			scored = append(scored, scoredSkill{skill: skill, score: score})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].skill.ID < scored[j].skill.ID
		}
		return scored[i].score > scored[j].score
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	result := make([]types.Skill, 0, len(scored))
	for _, item := range scored {
		result = append(result, item.skill)
	}
	return result
}

func matchesAny(text string, keywords []string) bool {
	text = strings.ToLower(text)
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

func tokenSet(text string) map[string]bool {
	result := make(map[string]bool)
	for _, token := range latinTokenPattern.FindAllString(strings.ToLower(text), -1) {
		result[token] = true
	}
	return result
}

func cjkBigrams(text string) map[string]bool {
	result := make(map[string]bool)
	var run []rune
	flush := func() {
		for i := 0; i+1 < len(run); i++ {
			result[string(run[i:i+2])] = true
		}
		run = run[:0]
	}
	for _, r := range text {
		if utf8.RuneLen(r) == 3 && r >= 0x4E00 && r <= 0x9FFF {
			run = append(run, r)
		} else if len(run) > 0 {
			flush()
		}
	}
	if len(run) > 0 {
		flush()
	}
	return result
}

func skillNames(skills []types.Skill) []string {
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		name := skill.ID
		if skill.Name != "" {
			name = skill.Name
		}
		names = append(names, name)
	}
	return names
}

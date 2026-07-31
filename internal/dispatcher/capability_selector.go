package dispatcher

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/good-fish-man/agent-runtime/internal/eino"
	"github.com/good-fish-man/agent-runtime/internal/tools"
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
	webKeywords = []string{
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

func selectBuiltinTools(text string, hasFiles bool) []string {
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
	add("AskUserQuestion")
	if hasFiles || matchesAny(text, codeReadKeywords) {
		add("Glob", "Grep", "Read")
	}
	if matchesAny(text, codeWriteKeywords) {
		add("Glob", "Grep", "Read", "Edit", "Write")
	}
	if matchesAny(text, commandKeywords) {
		add("Bash")
	}
	if needsWebAccess(text) {
		add("WebSearch", "WebFetch")
	}
	if matchesAny(text, browserAuthenticationKeywords) {
		add(tools.BrowserLoginToolName, tools.BrowserReadToolName, tools.BrowserCloseToolName)
	}
	if matchesAny(text, planKeywords) || matchesAny(text, researchPlanningKeywords) {
		add("TodoWrite", "EnterPlanMode", "ExitPlanMode")
	}
	if matchesAny(text, taskKeywords) {
		add("TaskCreate", "TaskGet", "TaskList", "TaskUpdate")
	}
	if matchesAny(text, waitKeywords) {
		add("Sleep")
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

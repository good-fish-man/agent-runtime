package research

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/good-fish-man/agent-runtime/internal/language"
)

// Kind identifies a deterministic research workflow.
type Kind string

const (
	KindNone       Kind = ""
	KindNews       Kind = "news"
	KindTravel     Kind = "travel"
	KindComparison Kind = "comparison"
	KindProcedure  Kind = "procedure"
	KindResearch   Kind = "research"
)

// Plan is the code-owned contract for one research pass.
type Plan struct {
	Kind             Kind
	Queries          []string
	SeedURLs         []string
	MinSources       int
	MaxSources       int
	Date             string
	ResolvedRequest  string
	ResearchGoal     string
	Constraints      []string
	ResponseLanguage string
}

var urlPattern = regexp.MustCompile(`https?://[^\s<>"']+`)

var structuredAnswerLabelPattern = regexp.MustCompile(`(?i)(fetch\s+now\?|count|sources?|立即获取|现在获取|数量|来源)\s*[：:]`)

var (
	chineseCredentialConversionPattern = regexp.MustCompile(`(?:想|希望|需要|要)?把?\s*([^，。！？,;；\n]{1,32}?(?:驾照|驾驶证|许可证|执照|资格证))\s*(?:换成|换为|更换为|转换为|转为|转成)\s*([^，。！？,;；\n]{1,32}?(?:驾照|驾驶证|许可证|执照|资格证))`)
	englishCredentialConversionPattern = regexp.MustCompile(`(?i)(?:convert|exchange)\s+(?:my\s+)?([^,.;!?]{1,48}?(?:driver'?s?\s+licen[cs]e|driving\s+licen[cs]e|permit))\s+(?:to|into|for)\s+([^,.;!?]{1,48}?(?:driver'?s?\s+licen[cs]e|driving\s+licen[cs]e|permit))`)
	chineseIdentityPattern             = regexp.MustCompile(`(?:我是|本人是)([^，。！？,;；\s]{1,20})`)
	chineseLocationPattern             = regexp.MustCompile(`(?:目前|现在)?在([^，。！？,;；\s]{1,20})(工作|居住|生活|留学)`)
)

var (
	newsKeywords       = []string{"新闻", "资讯", "头条", "要闻", "news", "headlines", "current events"}
	travelKeywords     = []string{"旅行", "旅游", "行程", "出行", "度假", "自驾", "机票", "酒店", "住宿", "攻略", "travel", "trip", "itinerary", "vacation", "flight", "hotel"}
	comparisonKeywords = []string{"推荐", "对比", "比较", "选型", "哪个好", "值得买吗", "购买建议", "recommend", "compare", "comparison", "best", "buying guide"}
	researchKeywords   = []string{"调研", "研究", "了解一下", "帮我了解", "查证", "核实", "搜索", "上网", "联网", "查询一下", "查一下", "来源", "引用", "research", "learn about", "tell me about", "investigate", "verify", "search online", "look up", "browse", "sources", "citations"}
	weatherKeywords    = []string{"天气", "气温", "weather", "forecast", "temperature"}
	procedureKeywords  = []string{
		"驾照", "驾驶证", "换证", "签证", "护照", "在留卡", "居留", "永住", "入籍", "移民", "税务", "社会保险", "社保", "养老金", "行政手续", "办理流程", "申请条件", "所需材料", "官方手续", "许可证", "执照", "资格认证",
		"driver's license", "driving license", "driving licence", "license conversion", "licence conversion", "visa", "passport", "residence permit", "immigration", "naturalization", "tax filing", "social security", "pension", "government procedure", "application process", "eligibility requirements", "required documents", "professional license",
	}
)

// Analyze recognizes research-heavy work and creates bounded search queries.
// Weather is intentionally handled by its location-aware path instead of a
// generic web query, which prevents searches such as "today weather".
func Analyze(prompt string, requestContext map[string]any, now time.Time) Plan {
	text := strings.TrimSpace(prompt)
	if text == "" {
		return Plan{}
	}
	localNow := userTime(now, requestContext)
	date := localNow.Format("2006-01-02")
	locale, _ := requestContext["locale"].(string)
	responseLanguage := language.Resolve(locale, text).Name
	urls := cleanURLs(urlPattern.FindAllString(text, -1))

	if containsAny(text, weatherKeywords) {
		return Plan{}
	}
	if containsAny(text, newsKeywords) {
		return newsPlan(date, locale, text, "", urls)
	}
	if containsAny(text, travelKeywords) {
		queries := uniqueQueries(text+" "+date, text+" 官方 交通 天气", text+" 价格 开放时间")
		if prefersEnglish(locale, text) {
			queries = uniqueQueries(text+" "+date, text+" official transport weather", text+" prices opening hours")
		}
		return Plan{
			Kind:             KindTravel,
			Queries:          queries,
			SeedURLs:         urls,
			MinSources:       3,
			MaxSources:       5,
			Date:             date,
			ResolvedRequest:  text,
			ResponseLanguage: responseLanguage,
		}
	}
	if containsAny(text, comparisonKeywords) {
		queries := uniqueQueries(text+" "+date, text+" 官方规格", text+" 独立评测")
		if prefersEnglish(locale, text) {
			queries = uniqueQueries(text+" "+date, text+" official specifications", text+" independent reviews")
		}
		return Plan{
			Kind:             KindComparison,
			Queries:          queries,
			SeedURLs:         urls,
			MinSources:       3,
			MaxSources:       5,
			Date:             date,
			ResolvedRequest:  text,
			ResponseLanguage: responseLanguage,
		}
	}
	if containsAny(text, procedureKeywords) {
		return procedurePlan(date, locale, text, urls, responseLanguage)
	}
	if len(urls) > 0 || containsAny(text, researchKeywords) {
		queries := uniqueQueries(text, text+" 官方来源")
		if prefersEnglish(locale, text) {
			queries = uniqueQueries(text, text+" official sources")
		}
		return Plan{
			Kind:             KindResearch,
			Queries:          queries,
			SeedURLs:         urls,
			MinSources:       2,
			MaxSources:       4,
			Date:             date,
			ResolvedRequest:  text,
			ResponseLanguage: responseLanguage,
		}
	}
	return Plan{}
}

func procedurePlan(date, locale, text string, urls []string, responseLanguage string) Plan {
	goal := procedureResearchGoal(text)
	constraints := procedureConstraints(text, goal)
	scope := strings.TrimSpace(strings.Join(append([]string{goal}, constraints...), " "))
	if scope == "" {
		scope = strings.TrimSpace(text)
	}
	year := date
	if len(date) >= 4 {
		year = date[:4]
	}
	queries := uniqueQueries(
		scope+" "+year+" 官方 申请资格 主管机关",
		scope+" 官方 所需材料 办理流程 预约 费用",
		scope+" 官方 受理地点 审查 考试 地方差异 注意事项",
	)
	if containsDrivingLicence(goal) {
		term := "外国免許切替"
		queries = uniqueQueries(
			scope+" "+term+" "+year+" 官方 申请资格 主管机关",
			scope+" "+term+" 官方 所需材料 翻译 预约 费用",
			scope+" "+term+" 官方 知识确认 技能确认 驾照中心 地方差异",
		)
	}
	if prefersEnglish(locale, text) {
		queries = uniqueQueries(
			scope+" "+year+" official eligibility competent authority",
			scope+" official required documents application process appointment fees",
			scope+" official local office assessment tests exceptions",
		)
	}
	return Plan{
		Kind: KindProcedure, Queries: queries, SeedURLs: urls, MinSources: 3, MaxSources: 5, Date: date,
		ResolvedRequest: text, ResearchGoal: goal, Constraints: constraints, ResponseLanguage: responseLanguage,
	}
}

func procedureResearchGoal(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if match := chineseCredentialConversionPattern.FindStringSubmatch(text); len(match) == 3 {
		return strings.TrimSpace(match[1]) + "换" + strings.TrimSpace(match[2])
	}
	if match := englishCredentialConversionPattern.FindStringSubmatch(text); len(match) == 3 {
		return strings.TrimSpace(match[1]) + " to " + strings.TrimSpace(match[2])
	}
	clauses := strings.FieldsFunc(text, func(r rune) bool {
		return strings.ContainsRune("，。！？,;；!?\n", r)
	})
	best, bestScore := "", -1
	for _, clause := range clauses {
		clause = strings.TrimSpace(clause)
		score := 0
		if containsAny(clause, procedureKeywords) {
			score += 10
		}
		if containsAny(clause, []string{"换", "办理", "申请", "转换", "更换", "怎么做", "如何", "convert", "exchange", "apply", "process"}) {
			score += 4
		}
		if score > bestScore || (score == bestScore && len([]rune(clause)) > len([]rune(best))) {
			best, bestScore = clause, score
		}
	}
	if best == "" {
		best = text
	}
	for _, prefix := range []string{"请问", "请帮我", "帮我", "我想要", "我想", "想要", "想"} {
		best = strings.TrimSpace(strings.TrimPrefix(best, prefix))
	}
	return truncateRunes(best, 100)
}

func procedureConstraints(text, goal string) []string {
	constraints := make([]string, 0, 4)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || strings.EqualFold(value, goal) {
			return
		}
		for _, existing := range constraints {
			if strings.EqualFold(existing, value) {
				return
			}
		}
		constraints = append(constraints, value)
	}
	if match := chineseIdentityPattern.FindStringSubmatch(text); len(match) == 2 {
		add("申请人 " + strings.TrimSpace(match[1]))
	}
	if match := chineseLocationPattern.FindStringSubmatch(text); len(match) == 3 {
		add("当前在" + strings.TrimSpace(match[1]) + strings.TrimSpace(match[2]))
	}
	for _, clause := range strings.FieldsFunc(text, func(r rune) bool {
		return strings.ContainsRune("，。！？,;；!?\n", r)
	}) {
		clause = strings.TrimSpace(clause)
		if containsAny(clause, []string{"持有", "已有", "已经有", "currently hold", "already have"}) && !strings.Contains(goal, clause) {
			add(clause)
		}
	}
	return constraints
}

func containsDrivingLicence(value string) bool {
	return containsAny(value, []string{"驾照", "驾驶证", "driver's license", "driver license", "driving license", "driving licence"})
}

func truncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if limit <= 0 || len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit]))
}

// AnalyzeConversation carries short user refinements such as "Tokyo" or
// "自驾" into the latest research task instead of treating them as new tasks.
func AnalyzeConversation(prompt string, previousUserPrompts []string, requestContext map[string]any, now time.Time) Plan {
	if refinement, ok := structuredAnswerRefinement(prompt); ok {
		return refinePreviousPlan(refinement, previousUserPrompts, requestContext, now, true)
	}
	currentPlan := Analyze(prompt, requestContext, now)
	if currentPlan.Kind != KindNone {
		if currentPlan.Kind == KindNews {
			if scope := priorNewsScope(previousUserPrompts, requestContext, now); scope != "" {
				locale, _ := requestContext["locale"].(string)
				resolved := currentPlan.ResolvedRequest + "\nConversation scope: " + scope
				return newsPlan(currentPlan.Date, locale, resolved, scope, currentPlan.SeedURLs)
			}
		}
		return currentPlan
	}
	refinement := strings.TrimSpace(prompt)
	if refinement == "" || len([]rune(refinement)) > 80 {
		return Plan{}
	}
	return refinePreviousPlan(refinement, previousUserPrompts, requestContext, now, false)
}

func refinePreviousPlan(refinement string, previousUserPrompts []string, requestContext map[string]any, now time.Time, preserveSubject bool) Plan {
	for i := len(previousUserPrompts) - 1; i >= 0; i-- {
		previous := strings.TrimSpace(previousUserPrompts[i])
		if previous == "" || previous == refinement {
			continue
		}
		plan := Analyze(previous, requestContext, now)
		if plan.Kind == KindNone {
			continue
		}
		locale, _ := requestContext["locale"].(string)
		resolved := previous + "\nUser refinement: " + refinement
		if preserveSubject {
			plan.ResolvedRequest = resolved
			plan.Queries = uniqueQueries(previous+" "+plan.Date, previous+" official sources")
			if !prefersEnglish(locale, previous) {
				plan.Queries = uniqueQueries(previous+" "+plan.Date, previous+" 官方来源")
			}
			return plan
		}
		if plan.Kind == KindNews {
			return newsPlan(plan.Date, locale, resolved, refinement, plan.SeedURLs)
		}
		return Analyze(resolved, requestContext, now)
	}
	return Plan{}
}

// structuredAnswerRefinement recognizes the compact text produced by
// clarification cards. Field labels describe the UI, not the search subject,
// so only their selected values should refine the preceding research task.
func structuredAnswerRefinement(value string) (string, bool) {
	value = strings.TrimSpace(value)
	matches := structuredAnswerLabelPattern.FindAllStringIndex(value, -1)
	if len(matches) < 2 {
		return "", false
	}
	selections := make([]string, 0, len(matches))
	for i, match := range matches {
		end := len(value)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		selection := strings.TrimSpace(value[match[1]:end])
		selection = strings.Trim(selection, ";,，。 \t\n")
		if selection != "" {
			selections = append(selections, selection)
		}
	}
	if len(selections) == 0 {
		return "", true
	}
	return strings.Join(selections, "; "), true
}

func priorNewsScope(previousUserPrompts []string, requestContext map[string]any, now time.Time) string {
	for i := len(previousUserPrompts) - 1; i >= 1; i-- {
		candidate := strings.TrimSpace(previousUserPrompts[i])
		if candidate == "" || len([]rune(candidate)) > 40 || Analyze(candidate, requestContext, now).Kind != KindNone || isAcknowledgement(candidate) {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if Analyze(previousUserPrompts[j], requestContext, now).Kind == KindNews {
				return candidate
			}
		}
	}
	return ""
}

func isAcknowledgement(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, acknowledgement := range []string{"好", "好的", "可以", "谢谢", "继续", "ok", "okay", "thanks", "continue"} {
		if value == acknowledgement {
			return true
		}
	}
	return false
}

func newsPlan(date, locale, prompt, scope string, urls []string) Plan {
	queries := []string{
		fmt.Sprintf("%s 今日要闻", date),
		fmt.Sprintf("%s 国际新闻", date),
		fmt.Sprintf("%s 科技 财经 新闻", date),
	}
	if prefersEnglish(locale, prompt) {
		queries = []string{
			fmt.Sprintf("top news %s", date),
			fmt.Sprintf("world news %s", date),
			fmt.Sprintf("technology business news %s", date),
		}
	}
	if scope != "" {
		queries = []string{
			fmt.Sprintf("%s %s 今日新闻", date, scope),
			fmt.Sprintf("%s %s 社会 经济 新闻", date, scope),
			fmt.Sprintf("%s %s 科技 商业 新闻", date, scope),
		}
		if prefersEnglish(locale, prompt) {
			queries = []string{
				fmt.Sprintf("%s news %s", scope, date),
				fmt.Sprintf("%s society economy news %s", scope, date),
				fmt.Sprintf("%s technology business news %s", scope, date),
			}
		}
	}
	return Plan{
		Kind: KindNews, Queries: queries, SeedURLs: urls, MinSources: 4, MaxSources: 6, Date: date,
		ResolvedRequest: prompt, ResponseLanguage: language.Resolve(locale, prompt).Name,
	}
}

func prefersEnglish(locale, text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return false
		}
	}
	return strings.HasPrefix(strings.ToLower(locale), "en") || locale == ""
}

func userTime(now time.Time, requestContext map[string]any) time.Time {
	if timezone, _ := requestContext["timezone"].(string); timezone != "" {
		if location, err := time.LoadLocation(timezone); err == nil {
			return now.In(location)
		}
	}
	return now
}

func containsAny(text string, keywords []string) bool {
	lower := strings.ToLower(text)
	for _, keyword := range keywords {
		if strings.Contains(lower, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func uniqueQueries(values ...string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.Join(strings.Fields(value), " ")
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func cleanURLs(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		value = strings.TrimRight(value, ".,;:!?，。；：！？)")
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

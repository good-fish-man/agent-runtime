package intent

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type keywordCategory string

const (
	categoryWorkspaceRead             keywordCategory = "workspace_read"
	categoryWorkspaceWrite            keywordCategory = "workspace_write"
	categoryCommand                   keywordCategory = "command"
	categoryLocalFileSearch           keywordCategory = "local_file_search"
	categoryLocalFileAction           keywordCategory = "local_file_action"
	categoryLocalFilePlace            keywordCategory = "local_file_place"
	categoryLocalFileObject           keywordCategory = "local_file_object"
	categoryOpenAction                keywordCategory = "open_action"
	categoryDesktopObject             keywordCategory = "desktop_object"
	categoryBrowserAction             keywordCategory = "browser_action"
	categoryBrowserObject             keywordCategory = "browser_object"
	categoryBrowserFollowUp           keywordCategory = "browser_follow_up"
	categoryBrowserMediaContext       keywordCategory = "browser_media_context"
	categoryBrowserObservation        keywordCategory = "browser_observation"
	categoryWeb                       keywordCategory = "web"
	categoryWebTemporal               keywordCategory = "web_temporal"
	categoryWebMutableFact            keywordCategory = "web_mutable_fact"
	categoryWebOfficialProcedure      keywordCategory = "web_official_procedure"
	categoryWebRecommendation         keywordCategory = "web_recommendation"
	categoryResearchPlanning          keywordCategory = "research_planning"
	categoryWebExternalKnowledge      keywordCategory = "web_external_knowledge"
	categoryWebQuestion               keywordCategory = "web_question"
	categoryBrowserAuthentication     keywordCategory = "browser_authentication"
	categoryBrowserClose              keywordCategory = "browser_close"
	categoryBrowserDownload           keywordCategory = "browser_download"
	categoryBrowserScreenshot         keywordCategory = "browser_screenshot"
	categoryPlan                      keywordCategory = "plan"
	categoryTask                      keywordCategory = "task"
	categoryWait                      keywordCategory = "wait"
	categoryScheduledTask             keywordCategory = "scheduled_task"
	categoryPersistentGoal            keywordCategory = "persistent_goal"
	categoryContextualMediaReply      keywordCategory = "contextual_media_reply"
	categoryInformationalRequest      keywordCategory = "informational_request"
	categoryPoliteBrowserActionPrefix keywordCategory = "polite_browser_action_prefix"
	categoryConversationRequestPrefix keywordCategory = "conversation_request_prefix"
	categoryGreeting                  keywordCategory = "greeting"
)

var knownKeywordCategories = map[keywordCategory]struct{}{
	categoryWorkspaceRead: {}, categoryWorkspaceWrite: {}, categoryCommand: {},
	categoryLocalFileSearch: {}, categoryLocalFileAction: {}, categoryLocalFilePlace: {}, categoryLocalFileObject: {},
	categoryOpenAction: {}, categoryDesktopObject: {}, categoryBrowserAction: {}, categoryBrowserObject: {},
	categoryBrowserFollowUp: {}, categoryBrowserMediaContext: {}, categoryBrowserObservation: {},
	categoryWeb: {}, categoryWebTemporal: {}, categoryWebMutableFact: {}, categoryWebOfficialProcedure: {},
	categoryWebRecommendation: {}, categoryResearchPlanning: {}, categoryWebExternalKnowledge: {}, categoryWebQuestion: {},
	categoryBrowserAuthentication: {}, categoryBrowserClose: {}, categoryBrowserDownload: {}, categoryBrowserScreenshot: {},
	categoryPlan: {}, categoryTask: {}, categoryWait: {}, categoryScheduledTask: {}, categoryPersistentGoal: {},
	categoryContextualMediaReply: {}, categoryInformationalRequest: {}, categoryPoliteBrowserActionPrefix: {},
	categoryConversationRequestPrefix: {}, categoryGreeting: {},
}

type languagePackFile struct {
	Version  int                          `yaml:"version"`
	Locale   string                       `yaml:"locale"`
	Aliases  []string                     `yaml:"aliases"`
	Keywords map[keywordCategory][]string `yaml:"keywords"`
}

type languagePack struct {
	keywords map[keywordCategory][]string
}

// Catalog is an immutable set of locale-specific deterministic intent terms.
// It is safe to share across dispatchers and concurrent requests.
type Catalog struct {
	byLocale map[string]*languagePack
	locales  []string
}

// LoadLanguagePacks reads all .yaml and .yml files in dir. An empty directory
// path disables external language packs. Invalid files fail closed so a typo
// cannot silently alter production routing.
func LoadLanguagePacks(dir string) (*Catalog, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return &Catalog{byLocale: map[string]*languagePack{}}, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read intent language packs %q: %w", dir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	catalog := &Catalog{byLocale: make(map[string]*languagePack)}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("read intent language pack %q: %w", path, readErr)
		}
		pack, decodeErr := decodeLanguagePack(data)
		if decodeErr != nil {
			return nil, fmt.Errorf("parse intent language pack %q: %w", path, decodeErr)
		}
		if err := catalog.add(pack); err != nil {
			return nil, fmt.Errorf("register intent language pack %q: %w", path, err)
		}
	}
	return catalog, nil
}

func decodeLanguagePack(data []byte) (languagePackFile, error) {
	var file languagePackFile
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		return file, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return file, fmt.Errorf("multiple YAML documents are not supported")
		}
		return file, err
	}
	if file.Version != 1 {
		return file, fmt.Errorf("version must be 1")
	}
	file.Locale = normalizeLocale(file.Locale)
	if file.Locale == "" {
		return file, fmt.Errorf("locale is required")
	}
	if len(file.Keywords) == 0 {
		return file, fmt.Errorf("keywords must not be empty")
	}
	for category, values := range file.Keywords {
		if _, ok := knownKeywordCategories[category]; !ok {
			return file, fmt.Errorf("unknown keyword category %q", category)
		}
		file.Keywords[category] = normalizeKeywords(values)
		if len(file.Keywords[category]) == 0 {
			return file, fmt.Errorf("keyword category %q must not be empty", category)
		}
	}
	for index, alias := range file.Aliases {
		file.Aliases[index] = normalizeLocale(alias)
		if file.Aliases[index] == "" {
			return file, fmt.Errorf("aliases must not contain an empty locale")
		}
	}
	return file, nil
}

func (c *Catalog) add(file languagePackFile) error {
	pack := &languagePack{keywords: file.Keywords}
	identifiers := append([]string{file.Locale}, file.Aliases...)
	for _, identifier := range identifiers {
		if _, exists := c.byLocale[identifier]; exists {
			return fmt.Errorf("locale or alias %q is already registered", identifier)
		}
		c.byLocale[identifier] = pack
	}
	c.locales = append(c.locales, file.Locale)
	sort.Strings(c.locales)
	return nil
}

func (c *Catalog) keywords(locale string, category keywordCategory) []string {
	if c == nil || len(c.byLocale) == 0 {
		return nil
	}
	locale = normalizeLocale(locale)
	pack := c.byLocale[locale]
	if pack == nil {
		if base, _, ok := strings.Cut(locale, "-"); ok {
			pack = c.byLocale[base]
		}
	}
	if pack == nil {
		return nil
	}
	return pack.keywords[category]
}

// Locales returns the canonical locales loaded into the catalog.
func (c *Catalog) Locales() []string {
	if c == nil {
		return nil
	}
	return append([]string(nil), c.locales...)
}

func normalizeLocale(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.ReplaceAll(value, "_", "-")
}

func normalizeKeywords(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

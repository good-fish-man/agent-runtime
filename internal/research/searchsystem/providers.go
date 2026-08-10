package searchsystem

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/constant"
)

const maxProviderResponseBytes = 4 << 20

type ProviderError struct {
	Provider   string
	StatusCode int
	Code       string
	Message    string
}

func (e *ProviderError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("%s provider %s (HTTP %d): %s", e.Provider, e.Code, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("%s provider %s: %s", e.Provider, e.Code, e.Message)
}

type githubProvider struct {
	client  *http.Client
	baseURL string
	token   string
}

func NewGitHubProvider(client *http.Client, baseURL, token string) Provider {
	return &githubProvider{client: providerClient(client), baseURL: defaultString(baseURL, constant.GitHubAPIBase), token: strings.TrimSpace(token)}
}

func (*githubProvider) Name() string     { return "github-api" }
func (*githubProvider) Kind() SourceKind { return SourceGitHub }

func (p *githubProvider) Search(ctx context.Context, query Query, count int) ([]Hit, error) {
	endpoint := strings.TrimRight(p.baseURL, "/") + "/search/repositories"
	values := url.Values{"q": {query.Text}, "per_page": {strconv.Itoa(providerCount(count))}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Athena-Research-Agent")
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	var payload struct {
		Items []struct {
			FullName    string `json:"full_name"`
			HTMLURL     string `json:"html_url"`
			Description string `json:"description"`
			Language    string `json:"language"`
			Stars       int    `json:"stargazers_count"`
			UpdatedAt   string `json:"updated_at"`
		} `json:"items"`
	}
	if err = doProviderJSON(ctx, p.client, p.Name(), req, &payload); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(payload.Items))
	for i, item := range payload.Items {
		snippet := strings.TrimSpace(item.Description)
		if item.Language != "" || item.Stars > 0 {
			snippet = fmt.Sprintf("%s Language: %s. Stars: %d.", snippet, item.Language, item.Stars)
		}
		publishedAt, _ := time.Parse(time.RFC3339, item.UpdatedAt)
		hits = append(hits, Hit{QueryID: query.ID, Provider: p.Name(), Kind: SourceGitHub, Title: item.FullName, URL: item.HTMLURL, Snippet: strings.TrimSpace(snippet), Priority: query.Priority, SearchRank: i + 1, PublishedAt: publishedAt})
	}
	return hits, nil
}

type wikipediaProvider struct {
	client  *http.Client
	baseURL string
}

func NewWikipediaProvider(client *http.Client, baseURL string) Provider {
	return &wikipediaProvider{client: providerClient(client), baseURL: strings.TrimRight(baseURL, "/")}
}

func (*wikipediaProvider) Name() string     { return "wikipedia" }
func (*wikipediaProvider) Kind() SourceKind { return SourceGeneral }

func (p *wikipediaProvider) Search(ctx context.Context, query Query, count int) ([]Hit, error) {
	baseURL := p.baseURL
	if baseURL == "" {
		baseURL = constant.WikipediaEnglishBase
		if containsHan(query.Text) {
			baseURL = constant.WikipediaChineseBase
		}
	}
	values := url.Values{
		"action": {"query"}, "list": {"search"}, "format": {"json"}, "utf8": {"1"},
		"srsearch": {query.Text}, "srlimit": {strconv.Itoa(providerCount(count))},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/w/api.php?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Athena-Research-Agent/2.0")
	var payload struct {
		Query struct {
			Search []struct {
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
			} `json:"search"`
		} `json:"query"`
	}
	if err = doProviderJSON(ctx, p.client, p.Name(), req, &payload); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(payload.Query.Search))
	for i, item := range payload.Query.Search {
		pageURL := strings.TrimRight(baseURL, "/") + "/wiki/" + url.PathEscape(strings.ReplaceAll(item.Title, " ", "_"))
		hits = append(hits, Hit{QueryID: query.ID, Provider: p.Name(), Kind: SourceGeneral, Title: item.Title, URL: pageURL, Snippet: stripProviderHTML(item.Snippet), Priority: query.Priority, SearchRank: i + 1})
	}
	return hits, nil
}

type arxivProvider struct {
	client  *http.Client
	baseURL string
}

func NewArxivProvider(client *http.Client, baseURL string) Provider {
	return &arxivProvider{client: providerClient(client), baseURL: defaultString(baseURL, constant.ArxivAPIURL)}
}

func (*arxivProvider) Name() string     { return "arxiv" }
func (*arxivProvider) Kind() SourceKind { return SourceAcademic }

func (p *arxivProvider) Search(ctx context.Context, query Query, count int) ([]Hit, error) {
	values := url.Values{
		"search_query": {"all:" + query.Text}, "start": {"0"},
		"max_results": {strconv.Itoa(providerCount(count))}, "sortBy": {"relevance"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Athena-Research-Agent/2.0")
	resp, body, err := doProviderRequest(ctx, p.client, p.Name(), req)
	if err != nil {
		return nil, err
	}
	_ = resp
	var feed struct {
		Entries []struct {
			ID        string `xml:"id"`
			Title     string `xml:"title"`
			Summary   string `xml:"summary"`
			Published string `xml:"published"`
		} `xml:"entry"`
	}
	if err = xml.Unmarshal(body, &feed); err != nil {
		return nil, &ProviderError{Provider: p.Name(), Code: "invalid_response", Message: err.Error()}
	}
	hits := make([]Hit, 0, len(feed.Entries))
	for i, entry := range feed.Entries {
		publishedAt, _ := time.Parse(time.RFC3339, strings.TrimSpace(entry.Published))
		hits = append(hits, Hit{QueryID: query.ID, Provider: p.Name(), Kind: SourceAcademic, Title: strings.Join(strings.Fields(entry.Title), " "), URL: strings.TrimSpace(entry.ID), Snippet: strings.Join(strings.Fields(entry.Summary), " "), Priority: query.Priority, SearchRank: i + 1, PublishedAt: publishedAt})
	}
	return hits, nil
}

type gdeltProvider struct {
	client  *http.Client
	baseURL string
}

func NewGDELTProvider(client *http.Client, baseURL string) Provider {
	return &gdeltProvider{client: providerClient(client), baseURL: defaultString(baseURL, constant.GDELTDocAPIURL)}
}

func (*gdeltProvider) Name() string     { return "gdelt-news" }
func (*gdeltProvider) Kind() SourceKind { return SourceNews }

func (p *gdeltProvider) Search(ctx context.Context, query Query, count int) ([]Hit, error) {
	values := url.Values{
		"query": {query.Text}, "mode": {"ArtList"}, "format": {"json"},
		"maxrecords": {strconv.Itoa(providerCount(count))}, "sort": {"HybridRel"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Athena-Research-Agent/2.0")
	var payload struct {
		Articles []struct {
			URL           string `json:"url"`
			Title         string `json:"title"`
			SeenDate      string `json:"seendate"`
			Domain        string `json:"domain"`
			SourceCountry string `json:"sourcecountry"`
			Language      string `json:"language"`
		} `json:"articles"`
	}
	if err = doProviderJSON(ctx, p.client, p.Name(), req, &payload); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(payload.Articles))
	for i, article := range payload.Articles {
		snippet := fmt.Sprintf("Published/seen: %s. Source: %s (%s, %s).", article.SeenDate, article.Domain, article.SourceCountry, article.Language)
		publishedAt, _ := time.Parse("20060102T150405Z", article.SeenDate)
		hits = append(hits, Hit{QueryID: query.ID, Provider: p.Name(), Kind: SourceNews, Title: article.Title, URL: article.URL, Snippet: snippet, Priority: query.Priority, SearchRank: i + 1, PublishedAt: publishedAt})
	}
	return hits, nil
}

func doProviderJSON(ctx context.Context, client *http.Client, provider string, req *http.Request, target any) error {
	_, body, err := doProviderRequest(ctx, client, provider, req)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(body, target); err != nil {
		return &ProviderError{Provider: provider, Code: "invalid_response", Message: err.Error()}
	}
	return nil
}

func doProviderRequest(ctx context.Context, client *http.Client, provider string, req *http.Request) (*http.Response, []byte, error) {
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, &ProviderError{Provider: provider, Code: "network_error", Message: err.Error()}
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxProviderResponseBytes))
	if readErr != nil {
		return resp, nil, &ProviderError{Provider: provider, StatusCode: resp.StatusCode, Code: "read_error", Message: readErr.Error()}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		code := "http_error"
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
			code = "rate_limited"
		}
		return resp, body, &ProviderError{Provider: provider, StatusCode: resp.StatusCode, Code: code, Message: strings.TrimSpace(string(body))}
	}
	return resp, body, nil
}

func providerClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func providerCount(count int) int {
	if count <= 0 {
		return 5
	}
	if count > 20 {
		return 20
	}
	return count
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimRight(value, "/")
}

func containsHan(value string) bool {
	for _, r := range value {
		if r >= '\u4e00' && r <= '\u9fff' {
			return true
		}
	}
	return false
}

func stripProviderHTML(value string) string {
	var out strings.Builder
	inTag := false
	for _, r := range value {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				out.WriteRune(r)
			}
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

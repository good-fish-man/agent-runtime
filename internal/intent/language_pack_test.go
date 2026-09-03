package intent

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLanguagePackUsesLocaleAndAliasWithoutCrossLocaleMatching(t *testing.T) {
	dir := t.TempDir()
	writeLanguagePack(t, dir, "ja.yaml", `
version: 1
locale: ja
aliases: [ja-JP]
keywords:
  web_mutable_fact: [天気予報]
`)
	catalog, err := LoadLanguagePacks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.Locales(); !reflect.DeepEqual(got, []string{"ja"}) {
		t.Fatalf("locales = %v, want [ja]", got)
	}

	parser := NewParser(catalog)
	parsed := parser.Parse(Request{Text: "東京の天気予報を教えて", Locale: "ja-JP"})
	if !parsed.HasSignal(SignalWebAccess) || parsed.Mode != ModeResearch {
		t.Fatalf("Japanese pack was not applied: %+v", parsed)
	}
	parsed = parser.Parse(Request{Text: "東京の天気予報を教えて", Locale: "fr-FR"})
	if parsed.HasSignal(SignalWebAccess) {
		t.Fatalf("Japanese terms leaked into an unrelated locale: %+v", parsed)
	}
}

func TestBundledJapaneseLanguagePack(t *testing.T) {
	catalog, err := LoadLanguagePacks(filepath.Join("..", "..", "config", "intent-languages"))
	if err != nil {
		t.Fatal(err)
	}
	parsed := NewParser(catalog).Parse(Request{Text: "今日の東京の天気を教えて", Locale: "ja-JP"})
	if !parsed.HasSignal(SignalWebAccess) || parsed.Mode != ModeResearch {
		t.Fatalf("bundled Japanese language pack was not applied: %+v", parsed)
	}
}

func TestLanguagePackPropagatesToBrowserHistory(t *testing.T) {
	dir := t.TempDir()
	writeLanguagePack(t, dir, "ja.yaml", `
version: 1
locale: ja
keywords:
  browser_action: [再生して]
  browser_object: [動画]
  browser_follow_up: [二番目]
`)
	catalog, err := LoadLanguagePacks(dir)
	if err != nil {
		t.Fatal(err)
	}

	parsed := NewParser(catalog).Parse(Request{
		Text:                 "二番目を再生して",
		Locale:               "ja-JP",
		ActiveBrowserSession: true,
		PreviousUserMessages: []string{"動画を再生して"},
	})
	if !parsed.HasSignal(SignalDirectBrowserControl) || parsed.Mode != ModeExecute {
		t.Fatalf("language pack was not propagated to browser history: %+v", parsed)
	}
}

func TestLoadLanguagePacksRejectsUnknownCategoryAndDuplicateAlias(t *testing.T) {
	t.Run("unknown category", func(t *testing.T) {
		dir := t.TempDir()
		writeLanguagePack(t, dir, "invalid.yaml", `
version: 1
locale: ja
keywords:
  imaginary_intent: [sample]
`)
		_, err := LoadLanguagePacks(dir)
		if err == nil || !strings.Contains(err.Error(), "unknown keyword category") {
			t.Fatalf("error = %v, want unknown keyword category", err)
		}
	})

	t.Run("duplicate alias", func(t *testing.T) {
		dir := t.TempDir()
		writeLanguagePack(t, dir, "one.yaml", `
version: 1
locale: ja
aliases: [shared]
keywords:
  greeting: [こんにちは]
`)
		writeLanguagePack(t, dir, "two.yaml", `
version: 1
locale: ko
aliases: [shared]
keywords:
  greeting: [안녕하세요]
`)
		_, err := LoadLanguagePacks(dir)
		if err == nil || !strings.Contains(err.Error(), "already registered") {
			t.Fatalf("error = %v, want duplicate locale or alias", err)
		}
	})
}

func writeLanguagePack(t *testing.T, dir, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.TrimSpace(contents)), 0600); err != nil {
		t.Fatal(err)
	}
}

package evidenceaudit

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	maxLineBytes = 4 << 20
	probeBytes   = 8 << 10
)

// Finding identifies a likely plaintext credential without retaining the
// matched value. Audit output must remain safe to attach to release evidence.
type Finding struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Rule string `json:"rule"`
}

type Report struct {
	ScannedFiles int       `json:"scanned_files"`
	Findings     []Finding `json:"findings"`
	Errors       []string  `json:"errors,omitempty"`
}

type detector struct {
	name    string
	pattern *regexp.Regexp
	value   int
}

var detectors = []detector{
	{name: "private-key", pattern: regexp.MustCompile(`(?i)-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`)},
	{name: "authorization", pattern: regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*(?:bearer|basic)\s+([^\s,;"']+)`), value: 1},
	{name: "cookie-header", pattern: regexp.MustCompile(`(?i)\b(?:set-cookie|cookie)\s*:\s*([^\r\n]+)`), value: 1},
	{name: "url-userinfo", pattern: regexp.MustCompile(`(?i)https?://[^\s/:@]+:([^\s/@]+)@`), value: 1},
	{name: "credential-field", pattern: regexp.MustCompile(`(?i)["']?(?:password|passwd|pwd|api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret|secret[_-]?key|session[_-]?token|device[_-]?token|db[_-]?password|密码|口令|api密钥|访问令牌|刷新令牌|设备令牌)["']?\s*[:=：]\s*["']?([^"'\s,，;；&}]+)`), value: 1},
	{name: "provider-token", pattern: regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{16,}|gh[pousr]_[A-Za-z0-9]{16,}|hf_[A-Za-z0-9]{16,}|npm_[A-Za-z0-9]{16,}|AIza[0-9A-Za-z_-]{20,}|AKIA[0-9A-Z]{16})\b`)},
	{name: "jwt", pattern: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)},
}

func Scan(paths []string) Report {
	report := Report{Findings: make([]Finding, 0)}
	for _, path := range paths {
		if err := scanPath(filepath.Clean(path), &report); err != nil {
			report.Errors = append(report.Errors, err.Error())
		}
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Path != report.Findings[j].Path {
			return report.Findings[i].Path < report.Findings[j].Path
		}
		if report.Findings[i].Line != report.Findings[j].Line {
			return report.Findings[i].Line < report.Findings[j].Line
		}
		return report.Findings[i].Rule < report.Findings[j].Rule
	})
	return report
}

func scanPath(path string, report *Report) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refuse symlink evidence path %s", path)
	}
	if !info.IsDir() {
		return scanFile(path, report)
	}
	return filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if err := scanFile(current, report); err != nil {
			return err
		}
		return nil
	})
}

func scanFile(path string, report *Report) error {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return scanZip(path, report)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return scanTarGzip(path, report)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open evidence %s: %w", path, err)
	}
	defer file.Close()
	return scanReader(path, file, report)
}

func scanZip(path string, report *Report) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open zip evidence %s: %w", path, err)
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() || entry.Mode()&os.ModeSymlink != 0 {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %s!%s: %w", path, entry.Name, err)
		}
		err = scanReader(path+"!"+filepath.ToSlash(entry.Name), reader, report)
		closeErr := reader.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return fmt.Errorf("close zip entry %s!%s: %w", path, entry.Name, closeErr)
		}
	}
	return nil
}

func scanTarGzip(path string, report *Report) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open archive evidence %s: %w", path, err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gzip evidence %s: %w", path, err)
	}
	defer gzipReader.Close()
	archive := tar.NewReader(gzipReader)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar evidence %s: %w", path, err)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		if err := scanReader(path+"!"+filepath.ToSlash(header.Name), io.LimitReader(archive, header.Size), report); err != nil {
			return err
		}
	}
}

func scanReader(path string, reader io.Reader, report *Report) error {
	buffered := bufio.NewReader(reader)
	probe, err := buffered.Peek(probeBytes)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return fmt.Errorf("probe evidence %s: %w", path, err)
	}
	if !isText(probe) {
		return nil
	}
	report.ScannedFiles++
	scanner := bufio.NewScanner(buffered)
	scanner.Buffer(make([]byte, 64<<10), maxLineBytes)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		for _, detector := range detectors {
			matches := detector.pattern.FindAllStringSubmatch(line, -1)
			for _, match := range matches {
				if detector.value > 0 && detector.value < len(match) && isSafeCredentialPlaceholder(detector.name, match[detector.value]) {
					continue
				}
				report.Findings = append(report.Findings, Finding{Path: path, Line: lineNumber, Rule: detector.name})
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan evidence %s: %w", path, err)
	}
	return nil
}

func isText(probe []byte) bool {
	if len(probe) == 0 {
		return true
	}
	for _, value := range probe {
		if value == 0 {
			return false
		}
	}
	return true
}

func isSafeCredentialPlaceholder(rule, value string) bool {
	value = strings.ToLower(strings.Trim(strings.TrimSpace(value), `"'`))
	switch value {
	case "[redacted]", "<redacted>", "redacted", "(redacted)", "***", "****", "*****", "xxxxx", "masked",
		"...", "@e?", "example", "placeholder",
		"text", "string", "bool", "boolean", "int", "integer", "null", "nil", "none", "unset", "empty":
		return true
	}
	if rule == "url-userinfo" {
		switch value {
		case "pass", "password", "passwd", "pwd", "secret", "token", "key":
			return true
		}
	}
	if (strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">")) ||
		strings.HasPrefix(value, "${") || strings.HasPrefix(value, "{{") || strings.HasPrefix(value, "$env:") ||
		strings.HasPrefix(value, "your-") || strings.HasPrefix(value, "your_") ||
		strings.HasPrefix(value, "example-") || strings.HasPrefix(value, "example_") ||
		strings.HasPrefix(value, "placeholder-") || strings.HasPrefix(value, "placeholder_") ||
		strings.HasPrefix(value, "?set") || strings.Contains(value, "_your_") {
		return true
	}
	for _, expression := range []string{"os.environ.get(", "os.getenv(", "config.get(", "viper.get(", "getenv("} {
		if strings.HasPrefix(value, expression) {
			return true
		}
	}
	return false
}

func (r Report) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

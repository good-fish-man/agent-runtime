package evidenceaudit

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanReportsRulesWithoutSecretValues(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "events.jsonl")
	providerToken := "sk-" + strings.Repeat("x", 24)
	contents := strings.Join([]string{
		`{"authorization":"Bearer ` + providerToken + `"}`,
		`password=[redacted]`,
		`url=https://athena:private-password@example.test/path`,
		`token_count=42`,
	}, "\n")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	report := Scan([]string{directory})
	if len(report.Errors) != 0 {
		t.Fatalf("errors = %v", report.Errors)
	}
	if len(report.Findings) < 2 {
		t.Fatalf("findings = %#v", report.Findings)
	}
	payload, err := report.JSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{providerToken, "private-password"} {
		if strings.Contains(string(payload), secret) {
			t.Fatalf("audit report leaked matched value %q", secret)
		}
	}
}

func TestScanReadsZipAndTarGzipEvidence(t *testing.T) {
	directory := t.TempDir()
	zipPath := filepath.Join(directory, "evidence.zip")
	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zipWriter := zip.NewWriter(zipFile)
	entry, err := zipWriter.Create("timeline.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte(`{"api_key":"fixture-plaintext-value"}`))
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatal(err)
	}

	tarPath := filepath.Join(directory, "artifacts.tar.gz")
	tarFile, err := os.Create(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(tarFile)
	tarWriter := tar.NewWriter(gzipWriter)
	data := []byte("-----BEGIN PRIVATE KEY-----\n")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "artifact.log", Mode: 0o600, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	_, _ = tarWriter.Write(data)
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tarFile.Close(); err != nil {
		t.Fatal(err)
	}

	report := Scan([]string{zipPath, tarPath})
	if len(report.Errors) != 0 || len(report.Findings) != 2 || report.ScannedFiles != 2 {
		t.Fatalf("report = %#v", report)
	}
}

func TestScanSkipsBinaryAndRejectsSymlinkRoot(t *testing.T) {
	directory := t.TempDir()
	binaryPath := filepath.Join(directory, "binary")
	if err := os.WriteFile(binaryPath, []byte{0, 's', 'k', '-', 's', 'e', 'c', 'r', 'e', 't'}, 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(directory, "link")
	if err := os.Symlink(binaryPath, linkPath); err != nil {
		t.Fatal(err)
	}
	report := Scan([]string{binaryPath})
	if report.ScannedFiles != 0 || len(report.Findings) != 0 || len(report.Errors) != 0 {
		t.Fatalf("binary report = %#v", report)
	}
	report = Scan([]string{linkPath})
	if len(report.Errors) != 1 {
		t.Fatalf("symlink report = %#v", report)
	}
}

func TestScanAllowsExplicitPlaceholdersButFindsFixedBootstrapCredential(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "release-corpus.txt")
	contents := strings.Join([]string{
		`DEFAULT_API_KEY="your-api-key"`,
		`AI_GATEWAY_API_KEY=gw_your_key_here`,
		`proxy=http://username:password@example.test`,
		`proxy=http://user:pass@example.test`,
		`APP_PASSWORD='...'`,
		`password=@e?`,
		`# : "${APP_PASSWORD:?Set APP_PASSWORD environment variable}"`,
		`secret_key = os.environ.get("S3_SECRET_KEY")`,
		`client_secret = config.get("client_secret")`,
		`rolpassword = text`,
		`password: athena`,
		`密码：<已脱敏>`,
		`密码：固定测试值`,
		`password: password`,
	}, "\n")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	report := Scan([]string{path})
	if len(report.Errors) != 0 {
		t.Fatalf("errors = %v", report.Errors)
	}
	if len(report.Findings) != 3 || report.Findings[0].Rule != "credential-field" || report.Findings[0].Line != 11 || report.Findings[1].Rule != "credential-field" || report.Findings[1].Line != 13 || report.Findings[2].Rule != "credential-field" || report.Findings[2].Line != 14 {
		t.Fatalf("expected only fixed English, Chinese, and weak credentials, report = %#v", report)
	}
}

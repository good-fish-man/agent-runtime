package dispatcher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/types"
)

func TestResolveSkillsDirUsesRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: demo\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d := &Dispatcher{req: &types.RunRequest{}, workDir: t.TempDir(), cfg: Config{SkillsDir: dir}}
	if got := d.resolveSkillsDir(); got != dir {
		t.Fatalf("resolveSkillsDir() = %q, want %q", got, dir)
	}
}

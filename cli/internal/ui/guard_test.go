package ui_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// bannedManagerDeps matches any import path that would prove charm — or another
// CLI-only heavy dependency — leaked into the operator manager's import graph.
// The manager builds from ./cmd and must never import cli/internal/ui (the only
// package allowed to depend on charm), so `go list -deps ./cmd` is a structural
// tripwire rather than a per-PR review burden.
var bannedManagerDeps = regexp.MustCompile(
	`charm|lipgloss|huh|bubble|ultraviolet|colorprofile|charmbracelet|ogmigo|kugo|internal/cardano/tx`,
)

func TestManagerImportGraphHasNoCharm(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	cmd := exec.Command("go", "list", "-deps", "./cmd")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps ./cmd failed: %v\n%s", err, out)
	}

	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if bannedManagerDeps.MatchString(line) {
			t.Errorf("manager import graph contains banned dependency %q; charm and CLI-only deps must stay out of ./cmd", line)
		}
	}
}

// legacyCharmImport matches the pre-v2 charmbracelet module paths the v2
// migration forbids anywhere under cli/. v2 lives at charm.land/*; importing
// github.com/charmbracelet/(lipgloss|huh|log) would silently pull a v0/v1 API.
var legacyCharmImport = regexp.MustCompile(`github\.com/charmbracelet/(lipgloss|huh|log)`)

func TestNoLegacyCharmPaths(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	err := filepath.WalkDir(filepath.Join(root, "cli"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for n, line := range strings.Split(string(data), "\n") {
			if legacyCharmImport.MatchString(line) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d imports a legacy charmbracelet path; use charm.land/* v2: %s", rel, n+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// moduleRoot walks up from the working directory to the first go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find module root")
		}
		dir = parent
	}
}

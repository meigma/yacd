package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// rawStreamWrite matches the banned process/raw-stream write forms: any
// os.Std*, any writer-less fmt.Print family, lipgloss.Print*, and any fmt.Fprint
// family targeting the raw commandContext.out/err streams. Helper functions that
// take an io.Writer parameter are intentionally not matched — they receive
// cc.io.Data() once routing lands. Data must reach stdout only through
// cc.io.Data/Encode; the human plane only through cc.io.* helpers.
var rawStreamWrite = regexp.MustCompile(
	`os\.Std(out|err|in)\b` +
		`|fmt\.Print(f|ln)?\(` +
		`|lipgloss\.Print` +
		`|fmt\.Fprint(f|ln)?\(\s*commandContext\.(out|err)\b`,
)

// passthroughPragma is the line-level escape hatch for the run/exec
// byte-transparent passthrough and the wallet_fund os.Stdout swap, plus the
// interim raw writes that later routing phases move onto cc.io. It is a per-line
// pragma (written "// ui-passthrough-ok"), never a whole-file allowlist; the
// bare token is matched so comment spacing does not matter.
const passthroughPragma = "ui-passthrough-ok"

func TestNoRawStreamWrites(t *testing.T) {
	t.Parallel()

	root := purityModuleRoot(t)
	dir := filepath.Join(root, "cli", "internal", "cli")

	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for n, line := range strings.Split(string(data), "\n") {
			// Skip full comment lines so prose mentioning os.Stdout / fmt.Print
			// does not trip the guard; trailing-comment pragmas on real write
			// lines are still honored below.
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if !rawStreamWrite.MatchString(line) || strings.Contains(line, passthroughPragma) {
				continue
			}
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s:%d raw stream write; route through cc.io (or tag // %s for passthrough): %s",
				rel, n+1, passthroughPragma, strings.TrimSpace(line))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// purityModuleRoot walks up from the working directory to the first go.mod.
func purityModuleRoot(t *testing.T) string {
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

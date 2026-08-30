package main_test

// Acceptance tests for TAE-80: adopt a .golangci.yml and clear the findings
// it produces. Written from the issue's acceptance criteria only.

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	return wd
}

func readConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), ".golangci.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected .golangci.yml at repo root (TAE-80 AC1): %v", err)
	}
	return string(data)
}

func TestGolangciConfigExists(t *testing.T) {
	path := filepath.Join(repoRoot(t), ".golangci.yml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected .golangci.yml at repo root (TAE-80 AC1): %v", err)
	}
}

func TestGolangciConfigDisablesOutputCaps(t *testing.T) {
	cfg := readConfig(t)
	mustHaveZeroKey(t, cfg, "max-issues-per-linter")
	mustHaveZeroKey(t, cfg, "max-same-issues")
}

func mustHaveZeroKey(t *testing.T, cfg, key string) {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `:\s*(\S+)`)
	m := re.FindStringSubmatch(cfg)
	if m == nil {
		t.Fatalf("expected a %q key in .golangci.yml (TAE-80 AC2)", key)
	}
	val := strings.TrimSpace(strings.TrimRight(m[1], "#"))
	n, err := strconv.Atoi(val)
	if err != nil || n != 0 {
		t.Fatalf("expected %s: 0 to disable the output cap so a reported count is the whole count, got %q (TAE-80 AC2)", key, m[1])
	}
}

func TestGolangciConfigDocumentsDeviations(t *testing.T) {
	cfg := readConfig(t)

	t.Run("cap keys carry a reason", func(t *testing.T) {
		for _, key := range []string{"max-issues-per-linter", "max-same-issues"} {
			if !lineOrPrecedingIsCommented(cfg, key+":") {
				t.Errorf("expected a comment explaining why %s is set to 0 (TAE-80 AC3)", key)
			}
		}
	})

	t.Run("depguard omission is explained", func(t *testing.T) {
		if !mentionedOnlyInComment(cfg, "depguard") {
			t.Error("expected depguard's omission to be named in a comment, with depguard itself not enabled as a live linter (TAE-80 AC3)")
		}
	})

	t.Run("revive stutter check disablement is explained", func(t *testing.T) {
		// Unlike depguard's omission, disabling revive's stutter check is done
		// via a live argument (e.g. disableStutteringCheck) -- "stutter" is
		// expected to appear in real YAML, not just in comments. What AC3
		// requires is that the deviation is explained *somewhere* in a comment.
		if !strings.Contains(strings.ToLower(cfg), "stutter") {
			t.Fatal("expected a mention of the disabled revive stutter check (TAE-80 AC3)")
		}
		if !hasCommentMentioning(cfg, "stutter") {
			t.Error("expected a comment explaining the revive stutter check disablement (TAE-80 AC3)")
		}
	})
}

// mentionedOnlyInComment reports whether word appears in the config, and every
// line it appears on has word preceded by a '#' -- i.e. it is discussed in
// prose, never configured as live YAML.
func mentionedOnlyInComment(cfg, word string) bool {
	lower := strings.ToLower(cfg)
	word = strings.ToLower(word)
	found := false
	for _, line := range strings.Split(lower, "\n") {
		idx := strings.Index(line, word)
		if idx == -1 {
			continue
		}
		found = true
		hashIdx := strings.Index(line, "#")
		if hashIdx == -1 || hashIdx > idx {
			return false
		}
	}
	return found
}

// hasCommentMentioning reports whether any comment in cfg (the "#" onward
// portion of a line) mentions word, case-insensitively.
func hasCommentMentioning(cfg, word string) bool {
	word = strings.ToLower(word)
	for _, line := range strings.Split(cfg, "\n") {
		hashIdx := strings.Index(line, "#")
		if hashIdx == -1 {
			continue
		}
		if strings.Contains(strings.ToLower(line[hashIdx:]), word) {
			return true
		}
	}
	return false
}

func lineOrPrecedingIsCommented(cfg, key string) bool {
	lines := strings.Split(cfg, "\n")
	for i, line := range lines {
		if !strings.Contains(line, key) {
			continue
		}
		if strings.Contains(line, "#") {
			return true
		}
		if i > 0 && strings.Contains(strings.TrimSpace(lines[i-1]), "#") {
			return true
		}
	}
	return false
}

func TestMakeLintUsesConfigAndFindsZeroIssues(t *testing.T) {
	out, err := exec.Command("make", "lint").CombinedOutput()
	if err != nil {
		t.Fatalf("expected `make lint` to run golangci-lint against .golangci.yml with zero findings (TAE-80 AC1/AC6): %v\n%s", err, out)
	}
}

func TestMakeLintBootstrapsWithoutPreinstalledGolangciLint(t *testing.T) {
	env := stripGolangciLintFromPath(t)
	before := untrackedFiles(t)

	cmd := exec.Command("make", "lint")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected `make lint` to bootstrap its own golangci-lint and succeed with PATH stripped of any pre-installed one (TAE-80 AC4/AC8): %v\n%s", err, out)
	}

	after := untrackedFiles(t)
	if diff := newEntries(before, after); len(diff) > 0 {
		t.Errorf("expected the bootstrapped golangci-lint binary to live in a directory ignored by git; found new untracked/modified paths after bootstrap: %v (TAE-80 AC4)", diff)
	}
}

func TestMakeVerifyGreenWithoutPreinstalledGolangciLint(t *testing.T) {
	// Shares make_targets_test.go's tae79VerifyGuardEnv rather than its own
	// guard: a nested `make verify` must skip both self-referential tests,
	// not just this one, or the two fan recursion out into each other's
	// uncovered case instead of stopping at depth 1.
	if os.Getenv(tae79VerifyGuardEnv) != "" {
		t.Skip("nested invocation via `make verify` -> `go test ./...`; skipping to avoid infinite recursion")
	}
	env := stripGolangciLintFromPath(t)
	env = append(env, tae79VerifyGuardEnv+"=1")
	cmd := exec.Command("make", "verify")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected `make verify` to be green on a machine with no golangci-lint on PATH (TAE-80 AC8): %v\n%s", err, out)
	}
}

func stripGolangciLintFromPath(t *testing.T) []string {
	t.Helper()
	origPath := os.Getenv("PATH")
	var kept []string
	for _, dir := range filepath.SplitList(origPath) {
		if _, err := os.Stat(filepath.Join(dir, "golangci-lint")); err == nil {
			continue
		}
		kept = append(kept, dir)
	}
	newPath := strings.Join(kept, string(os.PathListSeparator))

	env := os.Environ()
	result := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			continue
		}
		result = append(result, kv)
	}
	return append(result, "PATH="+newPath)
}

func untrackedFiles(t *testing.T) map[string]bool {
	t.Helper()
	out, err := exec.Command("git", "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	files := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		files[line] = true
	}
	return files
}

func newEntries(before, after map[string]bool) []string {
	var diff []string
	for k := range after {
		if !before[k] {
			diff = append(diff, k)
		}
	}
	return diff
}

var bareNolint = regexp.MustCompile(`//\s*nolint\s*($|[^:])`)

// TestNoBlanketNolintSuppressions inspects actual Go comments (via go/parser),
// never raw source text -- a string literal that happens to mention "nolint"
// (such as this very test's own failure messages) must not be mistaken for a
// suppression directive.
func TestNoBlanketNolintSuppressions(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return nil // a build-breaking file is a `make verify` failure, not this test's concern
		}
		rel, _ := filepath.Rel(root, path)
		for _, cg := range file.Comments {
			for _, c := range cg.List {
				if !strings.Contains(c.Text, "nolint") {
					continue
				}
				if bareNolint.MatchString(c.Text) {
					pos := fset.Position(c.Pos())
					t.Errorf("%s:%d: blanket //nolint without a linter list and reason -- only a narrow, reasoned suppression is allowed (TAE-80 AC7): %q", rel, pos.Line, c.Text)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

func TestGosecG106SuppressionsReferenceTAE22(t *testing.T) {
	path := filepath.Join(repoRoot(t), "internal", "ssh", "manager.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("internal/ssh/manager.go: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	for _, lineNo := range []int{620, 845} {
		if lineNo > len(lines) {
			t.Errorf("expected a //nolint:gosec suppression naming TAE-22 near line %d, but the file has only %d lines (TAE-80 AC7)", lineNo, len(lines))
			continue
		}
		start := max(0, lineNo-2)
		end := min(len(lines), lineNo+1)
		window := strings.Join(lines[start:end], "\n")
		if !strings.Contains(window, "nolint:gosec") || !strings.Contains(window, "TAE-22") {
			t.Errorf("expected internal/ssh/manager.go:%d to carry a //nolint:gosec suppression naming TAE-22 as the reason (TAE-80 AC7); got:\n%s", lineNo, window)
		}
	}
}

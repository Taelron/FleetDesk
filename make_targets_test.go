package main_test

// Acceptance tests for TAE-79: Makefile help, verify and regression targets.
// Written from the issue's acceptance criteria only -- no plan, no
// implementation, no Makefile or CI config were read to produce these.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// allTargetsAfterTAE79 is the target set TAE-79's decisions imply: the nine
// pre-existing targets named in the issue, minus `check` (renamed), plus
// `verify` (the rename), `regression` (new) and `help` (new).
var allTargetsAfterTAE79 = []string{
	"help", "test", "test-report", "build", "lint",
	"verify", "verify-nolint", "verify-lint-baseline", "verify-fresh-clone",
	"clean", "regression",
}

// --- make help / default goal (AC: "make help lists every target ... .DEFAULT_GOAL is help") ---

func TestMakeHelpListsEveryTarget(t *testing.T) {
	out, err := exec.Command("make", "help").CombinedOutput()
	if err != nil {
		t.Fatalf("expected `make help` to succeed and print a target listing (TAE-79 AC1): %v\n%s", err, out)
	}
	s := string(out)
	for _, name := range allTargetsAfterTAE79 {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
		if !re.MatchString(s) {
			t.Errorf("expected `make help` to list target %q (TAE-79 AC1); got:\n%s", name, s)
		}
	}
}

func TestMakeHelpGroupsTargets(t *testing.T) {
	out, err := exec.Command("make", "help").CombinedOutput()
	if err != nil {
		t.Fatalf("expected `make help` to succeed (TAE-79 AC1): %v\n%s", err, out)
	}
	// Grouped output reads as more than one paragraph. A single flat list
	// with no section breaks would fail this coarse proxy for "grouped".
	if strings.Count(strings.TrimRight(string(out), "\n"), "\n\n") < 1 {
		t.Errorf("expected `make help` output to be grouped into more than one section (TAE-79 AC1); got:\n%s", out)
	}
}

func TestDefaultGoalIsHelp(t *testing.T) {
	bare, err := exec.Command("make", "-n").CombinedOutput()
	if err != nil {
		t.Fatalf("expected bare `make -n` to resolve to a default goal (TAE-79 AC1/AC6): %v\n%s", err, bare)
	}
	help, err := exec.Command("make", "-n", "help").CombinedOutput()
	if err != nil {
		t.Fatalf("expected `make -n help` to succeed (TAE-79 AC1): %v\n%s", err, help)
	}
	if string(bare) != string(help) {
		t.Errorf("expected .DEFAULT_GOAL to be `help`, so bare `make` matches `make help` (TAE-79 AC6); bare `make -n`:\n%s\n---\n`make -n help`:\n%s", bare, help)
	}
}

// --- check removal / verify rename (AC: "check no longer exists as a target name") ---

func TestMakeCheckTargetRemoved(t *testing.T) {
	// -n (dry run): confirms the target is gone without ever executing a
	// recipe -- `check` currently exists and its recipe recursively invokes
	// `go test ./...`, so a real (non-dry-run) `make check` here would
	// re-run this whole suite from inside itself.
	out, err := exec.Command("make", "-n", "check").CombinedOutput()
	if err == nil {
		t.Fatalf("expected `check` to no longer exist as a target name (TAE-79 AC2); `make -n check` succeeded:\n%s", out)
	}
	s := strings.ToLower(string(out))
	if !strings.Contains(s, "no rule") || !strings.Contains(s, "check") {
		t.Errorf("expected `make check` to fail with a missing-target error, not some other failure (TAE-79 AC2): %v\n%s", err, out)
	}
}

// --- make -qp based static inspection helpers ---
//
// `make -qp` prints Make's internal database (every rule's prerequisites and
// recipe text) without running any recipe -- safe even for targets whose
// recipes clone the repo or hit the network. This lets ordering be verified
// without ever executing verify-fresh-clone or regression for real.

func makeDatabase(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("make", "-qp").Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("make -qp: %v", err)
		}
	}
	return string(out)
}

// targetBlock returns the database text for one target's rule (prerequisites
// and recipe), excluding the "target:" header line itself, up to the next
// blank line. Fails the test if the target has no rule at all.
func targetBlock(t *testing.T, db, target string) string {
	t.Helper()
	lines := strings.Split(db, "\n")
	header := regexp.MustCompile(`^` + regexp.QuoteMeta(target) + `:`)
	start := -1
	for i, line := range lines {
		if header.MatchString(line) {
			start = i
			break
		}
	}
	if start == -1 {
		t.Fatalf("expected a %q target in the Make database (TAE-79)", target)
	}
	end := len(lines)
	for j := start + 1; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "" {
			end = j
			break
		}
	}
	return strings.Join(lines[start+1:end], "\n")
}

// subTargetTokens, ordered longest-alternative-first so a compound name like
// verify-fresh-clone is never mistaken for a bare "verify" prefix match.
var subTargetMention = regexp.MustCompile(`\b(verify-fresh-clone|verify-lint-baseline|verify-nolint|verify|regression|build|lint|test-report|test)\b`)

var makeVarRef = regexp.MustCompile(`\$[({]([A-Za-z0-9_]+)[)}]`)

// makeVarValue looks up a variable's value from the `make -qp` database text,
// e.g. a line "REGRESSION_CONTROLS := verify-nolint verify-lint-baseline
// verify-fresh-clone".
func makeVarValue(db, name string) (string, bool) {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s*:?=\s*(.*)$`)
	m := re.FindStringSubmatch(db)
	if m == nil {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

// expandMakeVars substitutes $(VAR)/${VAR} references in block with VAR's
// value from db, so a recipe that names its sub-targets through a Makefile
// variable (rather than spelling them out) is still inspectable by name.
// Bounded passes handle one level of variable-referencing-variable.
func expandMakeVars(db, block string) string {
	for range 5 {
		replaced := false
		block = makeVarRef.ReplaceAllStringFunc(block, func(m string) string {
			name := makeVarRef.FindStringSubmatch(m)[1]
			if val, ok := makeVarValue(db, name); ok {
				replaced = true
				return val
			}
			return m
		})
		if !replaced {
			break
		}
	}
	return block
}

func orderedMentions(block string) []string {
	return subTargetMention.FindAllString(block, -1)
}

func filterTo(mentions []string, keep ...string) []string {
	set := map[string]bool{}
	for _, k := range keep {
		set[k] = true
	}
	var out []string
	for _, m := range mentions {
		if set[m] {
			out = append(out, m)
		}
	}
	return out
}

// --- verify ordering (AC: "make verify runs lint, build and test, in that order") ---

func TestMakeVerifyRunsLintBuildTestInOrder(t *testing.T) {
	db := makeDatabase(t)
	block := expandMakeVars(db, targetBlock(t, db, "verify"))
	got := filterTo(orderedMentions(block), "lint", "build", "test")
	want := []string{"lint", "build", "test"}
	if len(got) < 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("expected `verify` to invoke lint, build, test in that order (TAE-79 AC2); saw sub-target mentions %v in recipe:\n%s", got, block)
	}
}

// --- regression ordering (AC: "make regression runs verify, verify-nolint, verify-lint-baseline and verify-fresh-clone, in that order") ---

func TestMakeRegressionRunsAllFourInOrder(t *testing.T) {
	db := makeDatabase(t)
	block := expandMakeVars(db, targetBlock(t, db, "regression"))
	got := filterTo(orderedMentions(block), "verify", "verify-nolint", "verify-lint-baseline", "verify-fresh-clone")
	want := []string{"verify", "verify-nolint", "verify-lint-baseline", "verify-fresh-clone"}
	if len(got) < 4 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
		t.Errorf("expected `regression` to run verify, verify-nolint, verify-lint-baseline, verify-fresh-clone in that order (TAE-79 AC9); saw sub-target mentions %v in recipe:\n%s", got, block)
	}
}

// --- verify-fresh-clone runs verify, not regression (AC) ---

func TestVerifyFreshCloneRunsVerifyNotRegression(t *testing.T) {
	db := makeDatabase(t)
	block := expandMakeVars(db, targetBlock(t, db, "verify-fresh-clone"))
	mentions := orderedMentions(block)

	if !filterHas(mentions, "verify") {
		t.Errorf("expected `verify-fresh-clone` to run `make verify` inside its clone (TAE-79 AC11); recipe:\n%s", block)
	}
	if filterHas(mentions, "regression") {
		t.Errorf("expected `verify-fresh-clone` to NOT run `make regression` inside its clone -- that recurses without bound (TAE-79 AC11); recipe:\n%s", block)
	}
	if filterHas(mentions, "verify-nolint") || filterHas(mentions, "verify-lint-baseline") {
		t.Errorf("expected `verify-fresh-clone` to run only `verify` inside its clone, not the other regression-only targets (TAE-79 AC11); recipe:\n%s", block)
	}
}

func filterHas(mentions []string, name string) bool {
	return slices.Contains(mentions, name)
}

// --- verify's closing line names what it skips (AC) ---

// tae79VerifyGuardEnv prevents infinite recursion: `make verify` runs
// `go test ./...`, which would otherwise re-run this very test.
const tae79VerifyGuardEnv = "TAE79_MAKE_VERIFY_GUARD"

func TestMakeVerifySucceedsAndNamesSkippedWork(t *testing.T) {
	if os.Getenv(tae79VerifyGuardEnv) != "" {
		t.Skip("nested invocation via `make verify` -> `make test` -> `go test ./...`; skipping to avoid infinite recursion")
	}
	env := append(os.Environ(), tae79VerifyGuardEnv+"=1")
	cmd := exec.Command("make", "verify")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected `make verify` to succeed (TAE-79 AC2): %v\n%s", err, out)
	}
	s := string(out)
	for _, name := range []string{"verify-nolint", "verify-lint-baseline", "verify-fresh-clone"} {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
		if !re.MatchString(s) {
			t.Errorf("expected verify's closing line to name skipped regression-only target %q (TAE-79 AC7): %s", name, s)
		}
	}
	lower := strings.ToLower(s)
	mentionsManualTier := strings.Contains(lower, "ssh") || strings.Contains(lower, "azure") ||
		strings.Contains(lower, "k8s") || strings.Contains(lower, "kubernetes") ||
		strings.Contains(lower, "manual") || strings.Contains(lower, "integration")
	if !mentionsManualTier {
		t.Errorf("expected verify's closing line to name the manual integration tier (real SSH/Azure/K8s) (TAE-79 AC7): %s", s)
	}
}

// --- CI config (AC: fetch-depth: 0, `make regression` not raw go commands, no bin/tools cache) ---

func TestCIWorkflowRunsMakeRegressionWithFullHistory(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	path := filepath.Join(wd, ".github", "workflows", "ci.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected a CI workflow at %s: %v", path, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	steps := allWorkflowSteps(doc)
	if len(steps) == 0 {
		t.Fatalf("expected at least one step across all jobs in %s", path)
	}

	var (
		checkoutFetchDepthZero bool
		makeRegressionInvoked  bool
		rawGoCommandInvoked    bool
		cachesBinTools         bool
	)
	rawGoCmd := regexp.MustCompile(`(^|[;&|\s])go (test|build)\b`)

	for _, step := range steps {
		m, ok := step.(map[string]any)
		if !ok {
			continue
		}
		uses, _ := m["uses"].(string)
		with, _ := m["with"].(map[string]any)

		if strings.Contains(uses, "actions/checkout") && with != nil {
			if fd, ok := with["fetch-depth"]; ok && toStr(fd) == "0" {
				checkoutFetchDepthZero = true
			}
		}
		if strings.Contains(uses, "actions/cache") && with != nil {
			if p, ok := with["path"].(string); ok && strings.Contains(p, "bin/tools") {
				cachesBinTools = true
			}
		}
		if run, ok := m["run"].(string); ok {
			if strings.Contains(run, "make regression") {
				makeRegressionInvoked = true
			}
			if rawGoCmd.MatchString(run) {
				rawGoCommandInvoked = true
			}
		}
	}

	if !checkoutFetchDepthZero {
		t.Error("expected a checkout step with `fetch-depth: 0` (TAE-79 AC10); verify-lint-baseline checks out a historical ref and needs full history")
	}
	if !makeRegressionInvoked {
		t.Error("expected a CI step to invoke `make regression` (TAE-79 AC10)")
	}
	if rawGoCommandInvoked {
		t.Error("expected CI to invoke `make regression` rather than raw `go test`/`go build` commands (TAE-79 AC10)")
	}
	if cachesBinTools {
		t.Error("expected CI to NOT cache bin/tools/ -- a cached binary would stop CI from exercising the bootstrap verify-fresh-clone exists to prove (TAE-79 AC8)")
	}
}

func toStr(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func allWorkflowSteps(doc map[string]any) []any {
	var steps []any
	jobs, _ := doc["jobs"].(map[string]any)
	for _, jobVal := range jobs {
		job, ok := jobVal.(map[string]any)
		if !ok {
			continue
		}
		jobSteps, ok := job["steps"].([]any)
		if !ok {
			continue
		}
		steps = append(steps, jobSteps...)
	}
	return steps
}

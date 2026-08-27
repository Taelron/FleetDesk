package main_test

// Acceptance tests for TAE-37: CI adds the macOS leg. Written from the
// issue's acceptance criteria only -- no plan, no ci.yml, no Makefile were
// read to produce these.
//
// AC5 ("The PR links the CI run on its own head commit, with both legs
// green") has no local proxy: it is a fact about the PR description
// pointing at a GitHub Actions run that does not exist until the PR is
// opened against a pushed commit. There is nothing to assert in Go for it --
// it is verified by a human reading the PR, not by `go test`. No test
// function below claims to cover it.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func ciWorkflowDoc(t *testing.T) map[string]any {
	t.Helper()
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
	return doc
}

// regressionMatrixJob finds the job whose strategy.matrix has an axis
// listing both ubuntu-latest and macos-latest, and returns it. Fails the
// test, citing AC1, if no such job exists yet -- today's single ubuntu-only
// job has no matrix at all.
func regressionMatrixJob(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	jobs, _ := doc["jobs"].(map[string]any)
	for _, jobVal := range jobs {
		job, ok := jobVal.(map[string]any)
		if !ok {
			continue
		}
		strategy, ok := job["strategy"].(map[string]any)
		if !ok {
			continue
		}
		matrix, ok := strategy["matrix"].(map[string]any)
		if !ok {
			continue
		}
		for _, axisVal := range matrix {
			axis, ok := axisVal.([]any)
			if !ok {
				continue
			}
			hasUbuntu, hasMacOS := false, false
			for _, v := range axis {
				switch toStr(v) {
				case "ubuntu-latest":
					hasUbuntu = true
				case "macos-latest":
					hasMacOS = true
				}
			}
			if hasUbuntu && hasMacOS {
				return job
			}
		}
	}
	t.Fatalf("expected a job in ci.yml with a matrix axis listing both ubuntu-latest and macos-latest (TAE-37 AC1)")
	return nil
}

func TestCIMatrixCoversUbuntuAndMacOS(t *testing.T) {
	doc := ciWorkflowDoc(t)
	regressionMatrixJob(t, doc) // Fatals with the AC1 message if no such job exists
}

func TestCIMatrixRunsOnExpandsFromMatrix(t *testing.T) {
	doc := ciWorkflowDoc(t)
	job := regressionMatrixJob(t, doc)
	runsOn := toStr(job["runs-on"])
	if !strings.Contains(runsOn, "matrix.") {
		t.Errorf("expected runs-on to expand from the matrix (e.g. ${{ matrix.os }}), not a hardcoded OS, so one job definition produces both legs (TAE-37 AC1); got %q", runsOn)
	}
}

func TestCIMatrixFailFastDisabled(t *testing.T) {
	doc := ciWorkflowDoc(t)
	job := regressionMatrixJob(t, doc)
	strategy, _ := job["strategy"].(map[string]any)
	ff, ok := strategy["fail-fast"]
	if !ok {
		t.Fatalf("expected the matrix job's strategy to declare fail-fast (TAE-37 AC2)")
	}
	b, isBool := ff.(bool)
	if !isBool || b {
		t.Errorf("expected fail-fast: false, so one leg failing does not cancel the other (TAE-37 AC2); got %v", ff)
	}
}

func TestCIMatrixLegRunsMakeRegression(t *testing.T) {
	doc := ciWorkflowDoc(t)
	job := regressionMatrixJob(t, doc)
	steps, _ := job["steps"].([]any)
	found := false
	for _, s := range steps {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if run, ok := m["run"].(string); ok && strings.Contains(run, "make regression") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the matrix job to run `make regression`, the same target as ubuntu today, on every leg (TAE-37 AC1)")
	}
}

func TestCIMatrixLegChecksOutFullHistory(t *testing.T) {
	doc := ciWorkflowDoc(t)
	job := regressionMatrixJob(t, doc)
	steps, _ := job["steps"].([]any)
	found := false
	for _, s := range steps {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if uses, _ := m["uses"].(string); uses != "actions/checkout@v4" {
			continue
		}
		with, _ := m["with"].(map[string]any)
		if with != nil && toStr(with["fetch-depth"]) == "0" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the matrix job to use actions/checkout@v4 with fetch-depth: 0, as the single job does today (TAE-37 AC3)")
	}
}

func TestCIMatrixLegSetsUpGoFromModFile(t *testing.T) {
	doc := ciWorkflowDoc(t)
	job := regressionMatrixJob(t, doc)
	steps, _ := job["steps"].([]any)
	found := false
	for _, s := range steps {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if uses, _ := m["uses"].(string); uses != "actions/setup-go@v5" {
			continue
		}
		with, _ := m["with"].(map[string]any)
		if with != nil && toStr(with["go-version-file"]) == "go.mod" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the matrix job to use actions/setup-go@v5 with go-version-file: go.mod, as the single job does today (TAE-37 AC3)")
	}
}

// TestMacOSBootstrapsGolangciLintAndPassesChecksum is the only test in this
// file that executes anything rather than parsing ci.yml. It asserts the
// observable outcome AC4 requires -- `make lint` succeeds on darwin/arm64,
// bootstrapping its own golangci-lint -- without inspecting the Makefile
// recipe that produces it; which shasum/sha256sum branch it takes is an
// implementation detail this issue does not license reading here. On every
// other GOOS, including this machine, it is a skip, not a pass: the only
// place it can run for real is the macos-latest leg this issue adds to
// ci.yml, where `make regression` (which runs `make lint`) invokes
// `go test ./...` and reaches this file.
func TestMacOSBootstrapsGolangciLintAndPassesChecksum(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("only meaningful on darwin -- exercised for real by the macos-latest CI leg (TAE-37 AC4)")
	}
	env := stripGolangciLintFromPath(t)
	cmd := exec.Command("make", "lint")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected `make lint` to bootstrap golangci-lint through its shasum -a 256 branch and pass the checksum on darwin/arm64 (TAE-37 AC4): %v\n%s", err, out)
	}
}

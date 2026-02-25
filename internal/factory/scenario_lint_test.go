package attractor

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRootFromTestFile(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestScenarioLintPassesForDynamicModelAndMktemp(t *testing.T) {
	root := repoRootFromTestFile(t)
	lintScript := filepath.Join(root, "scripts", "scenarios", "lint_scenarios.sh")

	dir := t.TempDir()
	good := filepath.Join(dir, "agent_cli_user_scenarios.sh")
	if err := os.WriteFile(good, []byte(`#!/usr/bin/env bash
set -euo pipefail
SCENARIO_MODE="${SCENARIO_MODE:-live}"
ANTHROPIC_LIVE_MODEL="${ANTHROPIC_LIVE_MODEL:-}"
tmp="$(mktemp)"
rm -f "$tmp"
`), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", lintScript, dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected lint success, got error: %v\n%s", err, string(out))
	}
}

func TestScenarioLintRejectsHardcodedModelAndFixedTmp(t *testing.T) {
	root := repoRootFromTestFile(t)
	lintScript := filepath.Join(root, "scripts", "scenarios", "lint_scenarios.sh")

	dir := t.TempDir()
	bad := filepath.Join(dir, "agent_cli_user_scenarios.sh")
	if err := os.WriteFile(bad, []byte(`#!/usr/bin/env bash
set -euo pipefail
SCENARIO_MODE="${SCENARIO_MODE:-live}"
ANTHROPIC_LIVE_MODEL="${ANTHROPIC_LIVE_MODEL:-claude-3-5-haiku-20241022}"
echo x >/tmp/fixed-name.out
`), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", lintScript, dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected lint failure, got success:\n%s", string(out))
	}
	if !strings.Contains(string(out), "hardcoded ANTHROPIC_LIVE_MODEL default") {
		t.Fatalf("expected model-default lint error, got:\n%s", string(out))
	}
	if !strings.Contains(string(out), "fixed /tmp path detected") {
		t.Fatalf("expected fixed /tmp lint error, got:\n%s", string(out))
	}
}

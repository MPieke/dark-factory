package attractor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreflightScenarioClassifiesInfraFailure(t *testing.T) {
	root := repoRootFromTestFile(t)
	preflight := filepath.Join(root, "scripts", "scenarios", "preflight_scenario.sh")

	dir := t.TempDir()
	scenario := filepath.Join(dir, "scenario.sh")
	if err := os.WriteFile(scenario, []byte(`#!/usr/bin/env bash
set -euo pipefail
mode="${SCENARIO_MODE:-live}"
if [ "$mode" = "selftest" ]; then
  echo "selftest ok"
  exit 0
fi
echo "OPENAI_API_KEY is not set for live scenario checks" 1>&2
exit 1
`), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", preflight, scenario, dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected failure, got success:\n%s", string(out))
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected exit error, got: %v", err)
	}
	if exitErr.ExitCode() != 86 {
		t.Fatalf("expected infra exit code 86, got %d\n%s", exitErr.ExitCode(), string(out))
	}
	if !strings.Contains(string(out), "failure_class=infra") {
		t.Fatalf("expected infra classification output:\n%s", string(out))
	}
}

func TestPreflightScenarioClassifiesProductFailure(t *testing.T) {
	root := repoRootFromTestFile(t)
	preflight := filepath.Join(root, "scripts", "scenarios", "preflight_scenario.sh")

	dir := t.TempDir()
	scenario := filepath.Join(dir, "scenario.sh")
	if err := os.WriteFile(scenario, []byte(`#!/usr/bin/env bash
set -euo pipefail
mode="${SCENARIO_MODE:-live}"
if [ "$mode" = "selftest" ]; then
  echo "selftest ok"
  exit 0
fi
echo "expected behavior mismatch" 1>&2
exit 1
`), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", preflight, scenario, dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected failure, got success:\n%s", string(out))
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected exit error, got: %v", err)
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("expected product exit code 1, got %d\n%s", exitErr.ExitCode(), string(out))
	}
	if !strings.Contains(string(out), "failure_class=product") {
		t.Fatalf("expected product classification output:\n%s", string(out))
	}
}

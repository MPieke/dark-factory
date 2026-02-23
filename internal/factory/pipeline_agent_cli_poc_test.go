package attractor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeScenarioHarness(t *testing.T, workdir string) {
	t.Helper()
	writeFile(t, filepath.Join(workdir, "scripts/scenarios/preflight_provider_live.sh"), `#!/usr/bin/env bash
set -euo pipefail
if [ -f .preflight_fail ]; then
  echo "preflight failed" >&2
  exit 1
fi
echo "preflight ok"
`)
	writeFile(t, filepath.Join(workdir, "scripts/scenarios/agent_cli_component_checks.sh"), `#!/usr/bin/env bash
set -euo pipefail
if [ -f .component_fail ]; then
  echo "component failed" >&2
  exit 1
fi
echo "component ok"
`)
	writeFile(t, filepath.Join(workdir, "scripts/scenarios/agent_cli_user_scenarios.sh"), `#!/usr/bin/env bash
set -euo pipefail
if [ -f .user_fail ]; then
  echo "user scenarios failed" >&2
  exit 1
fi
echo "user scenarios ok"
`)
	writeFile(t, filepath.Join(workdir, "scripts/scenarios/preflight_scenario.sh"), `#!/usr/bin/env bash
set -euo pipefail
SCENARIO_SCRIPT="${1:-}"
APP_DIR="${2:-agent}"
bash "$SCENARIO_SCRIPT" "$APP_DIR"
`)
}

func buildAgentCliPocDOT(planJSON string) string {
	return `digraph AgentCliFactoryPOC {
  start [shape=Mdiamond];
  exit [shape=Msquare];
  exit_config_fail [shape=Msquare];

  preflight_live_deps [
    shape=parallelogram,
    type=tool,
    tool_command="bash scripts/scenarios/preflight_provider_live.sh"
  ];

  ensure_agent_dir [
    shape=parallelogram,
    type=tool,
    allowed_write_paths="agent/",
    tool_command="mkdir -p agent"
  ];

  implement [
    shape=box,
    "test.outcome"="success",
    "test.verification_plan_json"=` + strconv.Quote(planJSON) + `,
    allowed_write_paths="agent/"
  ];

  verify_plan [
    shape=parallelogram,
    type=verification,
    "verification.allowed_commands"="gofmt,test -f,go test"
  ];

  validate_component [
    shape=parallelogram,
    type=tool,
    tool_command="bash scripts/scenarios/agent_cli_component_checks.sh agent"
  ];

  validate_user_scenarios [
    shape=parallelogram,
    type=tool,
    tool_command="bash scripts/scenarios/preflight_scenario.sh scripts/scenarios/agent_cli_user_scenarios.sh agent"
  ];

  fix [
    shape=box,
    "test.outcome"="success",
    allowed_write_paths="agent/"
  ];

  start -> preflight_live_deps;
  preflight_live_deps -> ensure_agent_dir [condition="outcome=success"];
  preflight_live_deps -> exit_config_fail [condition="outcome=fail"];
  ensure_agent_dir -> implement [condition="outcome=success"];
  ensure_agent_dir -> exit_config_fail [condition="outcome=fail"];
  implement -> verify_plan [condition="outcome=success"];
  implement -> fix [condition="outcome=fail"];
  verify_plan -> validate_component [condition="outcome=success"];
  verify_plan -> fix [condition="outcome=fail"];
  validate_component -> validate_user_scenarios [condition="outcome=success"];
  validate_component -> fix [condition="outcome=fail"];
  validate_user_scenarios -> exit [condition="outcome=success"];
  validate_user_scenarios -> fix [condition="outcome=fail"];
  fix -> validate_component [condition="outcome=success"];
  fix -> fix [condition="outcome=fail"];
}`
}

func readStatusJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAgentCliPocPreflightFailureRoutesToConfigExit(t *testing.T) {
	t.Setenv("ATTRACTION_BACKEND", "fake")
	planJSON := `{"files":["agent/main.go"],"commands":["test -f agent/main.go"]}`
	workdir, runsdir, pipeline := setupRun(t, buildAgentCliPocDOT(planJSON))
	writeScenarioHarness(t, workdir)
	writeFile(t, filepath.Join(workdir, ".preflight_fail"), "1\n")

	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "poc-preflight-fail"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runsdir, "poc-preflight-fail", "exit_config_fail", "status.json")); err != nil {
		t.Fatalf("expected exit_config_fail reached: %v", err)
	}
}

func TestAgentCliPocVerificationCommandCWDMismatchFails(t *testing.T) {
	t.Setenv("ATTRACTION_BACKEND", "fake")
	t.Setenv("ATTRACTION_TEST_STOP_AFTER_NODE", "fix")
	planJSON := `{"files":["agent/main.go","agent/main_test.go"],"commands":["gofmt -w main.go main_test.go"]}`
	workdir, runsdir, pipeline := setupRun(t, buildAgentCliPocDOT(planJSON))
	writeScenarioHarness(t, workdir)
	writeFile(t, filepath.Join(workdir, "agent/main.go"), "package main\nfunc main(){}\n")
	writeFile(t, filepath.Join(workdir, "agent/main_test.go"), "package main\nimport \"testing\"\nfunc TestX(t *testing.T){}\n")

	err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "poc-verify-cwd-fail"})
	if err == nil || !strings.Contains(err.Error(), "test_stop") {
		t.Fatalf("expected deterministic stop at fix, got: %v", err)
	}
	status := readStatusJSON(t, filepath.Join(runsdir, "poc-verify-cwd-fail", "verify_plan", "status.json"))
	if status["outcome"] != "fail" {
		t.Fatalf("expected verify_plan fail, got: %+v", status)
	}
	reason, _ := status["failure_reason"].(string)
	if !strings.Contains(reason, "verification command failed") {
		t.Fatalf("unexpected failure reason: %s", reason)
	}
	if _, err := os.Stat(filepath.Join(runsdir, "poc-verify-cwd-fail", "fix", "status.json")); err != nil {
		t.Fatalf("expected fix stage to run after verify failure: %v", err)
	}
}

func TestAgentCliPocValidateUserFailureRoutesToFix(t *testing.T) {
	t.Setenv("ATTRACTION_BACKEND", "fake")
	t.Setenv("ATTRACTION_TEST_STOP_AFTER_NODE", "fix")
	planJSON := `{"files":["agent/main.go"],"commands":["test -f agent/main.go"]}`
	workdir, runsdir, pipeline := setupRun(t, buildAgentCliPocDOT(planJSON))
	writeScenarioHarness(t, workdir)
	writeFile(t, filepath.Join(workdir, "agent/main.go"), "package main\nfunc main(){}\n")
	writeFile(t, filepath.Join(workdir, ".user_fail"), "1\n")

	err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "poc-user-fail"})
	if err == nil || !strings.Contains(err.Error(), "test_stop") {
		t.Fatalf("expected deterministic stop at fix, got: %v", err)
	}
	status := readStatusJSON(t, filepath.Join(runsdir, "poc-user-fail", "validate_user_scenarios", "status.json"))
	if status["outcome"] != "fail" {
		t.Fatalf("expected validate_user_scenarios fail, got: %+v", status)
	}
	if _, err := os.Stat(filepath.Join(runsdir, "poc-user-fail", "fix", "status.json")); err != nil {
		t.Fatalf("expected fix stage to run after user scenario failure: %v", err)
	}
}

func TestAgentCliPocHappyPathReachesExit(t *testing.T) {
	t.Setenv("ATTRACTION_BACKEND", "fake")
	planJSON := `{"files":["agent/main.go","agent/main_test.go"],"commands":["gofmt -w agent/main.go agent/main_test.go"]}`
	workdir, runsdir, pipeline := setupRun(t, buildAgentCliPocDOT(planJSON))
	writeScenarioHarness(t, workdir)
	writeFile(t, filepath.Join(workdir, "agent/main.go"), "package main\nfunc main(){}\n")
	writeFile(t, filepath.Join(workdir, "agent/main_test.go"), "package main\nimport \"testing\"\nfunc TestX(t *testing.T){}\n")

	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "poc-happy"}); err != nil {
		t.Fatal(err)
	}
	verify := readStatusJSON(t, filepath.Join(runsdir, "poc-happy", "verify_plan", "status.json"))
	if verify["outcome"] != "success" {
		t.Fatalf("expected verify_plan success, got: %+v", verify)
	}
	if _, err := os.Stat(filepath.Join(runsdir, "poc-happy", "exit", "status.json")); err != nil {
		t.Fatalf("expected exit reached: %v", err)
	}
}

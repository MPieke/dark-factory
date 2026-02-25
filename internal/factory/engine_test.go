package attractor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readJSONLTypes(t *testing.T, p string) []string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	out := []string{}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := map[string]any{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m["type"].(string))
	}
	return out
}

func setupRun(t *testing.T, dot string) (workdir, runsdir, pipeline string) {
	t.Helper()
	root := t.TempDir()
	workdir = filepath.Join(root, "work")
	runsdir = filepath.Join(root, "runs")
	pipeline = filepath.Join(root, "pipeline.dot")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, pipeline, dot)
	return
}

func TestExecLinearArtifacts(t *testing.T) {
	t.Setenv("ATTRACTION_BACKEND", "fake")
	dot := `digraph G { start [shape=Mdiamond]; a [shape=box]; exit [shape=Msquare]; start -> a; a -> exit; }`
	workdir, runsdir, pipeline := setupRun(t, dot)
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runsdir, "r1", "a", "status.json")); err != nil {
		t.Fatal(err)
	}
	types := readJSONLTypes(t, filepath.Join(runsdir, "r1", "events.jsonl"))
	joined := strings.Join(types, ",")
	if !strings.Contains(joined, "PipelineStarted") || !strings.Contains(joined, "PipelineCompleted") {
		t.Fatalf("missing events: %s", joined)
	}
	if _, err := os.Stat(filepath.Join(runsdir, "r1", "checkpoint.json")); err != nil {
		t.Fatal(err)
	}
	traceTypes := readJSONLTypes(t, filepath.Join(runsdir, "r1", "trace.jsonl"))
	traceJoined := strings.Join(traceTypes, ",")
	for _, want := range []string{"SessionInitialized", "PipelineStarted", "NodeInputCaptured", "NodeOutputCaptured", "RouteEvaluated", "PipelineCompleted"} {
		if !strings.Contains(traceJoined, want) {
			t.Fatalf("missing trace type %s in %s", want, traceJoined)
		}
	}
}

func TestExecToolCapturesOutput(t *testing.T) {
	dot := `digraph G { start [shape=Mdiamond]; t [shape=parallelogram, tool_command="echo out && echo err 1>&2"]; exit [shape=Msquare]; start -> t; t -> exit; }`
	workdir, runsdir, pipeline := setupRun(t, dot)
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r2"}); err != nil {
		t.Fatal(err)
	}
	ob, _ := os.ReadFile(filepath.Join(runsdir, "r2", "t", "tool.stdout.txt"))
	eb, _ := os.ReadFile(filepath.Join(runsdir, "r2", "t", "tool.stderr.txt"))
	if !strings.Contains(string(ob), "out") || !strings.Contains(string(eb), "err") {
		t.Fatalf("unexpected tool output: %q %q", string(ob), string(eb))
	}
}

func TestExecRoutingByOutcome(t *testing.T) {
	t.Setenv("ATTRACTION_BACKEND", "fake")
	dot := `digraph G {
	  start [shape=Mdiamond];
	  a [shape=box, "test.outcome"="fail"];
	  exit_ok [shape=Msquare];
	  exit_fail [shape=Msquare];
	  start -> a;
	  a -> exit_fail [condition="outcome=fail"];
	  a -> exit_ok [condition="outcome=success"];
	}`
	workdir, runsdir, pipeline := setupRun(t, dot)
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r3"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runsdir, "r3", "exit_fail", "status.json")); err != nil {
		t.Fatal("expected exit_fail reached")
	}
}

func TestExecWeightTieBreak(t *testing.T) {
	t.Setenv("ATTRACTION_BACKEND", "fake")
	dot := `digraph G {
	start [shape=Mdiamond]; a [shape=box]; b [shape=box]; c [shape=box]; exit [shape=Msquare];
	start -> a;
	a -> b [weight=2];
	a -> c [weight=1];
	b -> exit;
	c -> exit;
	}`
	workdir, runsdir, pipeline := setupRun(t, dot)
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r4"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runsdir, "r4", "b", "status.json")); err != nil {
		t.Fatal("expected b chosen")
	}
}

func TestRetryHonored(t *testing.T) {
	t.Setenv("ATTRACTION_BACKEND", "fake")
	dot := `digraph G { start [shape=Mdiamond]; a [shape=box, max_retries=2, "test.outcome_sequence"="retry,retry,success"]; exit [shape=Msquare]; start -> a; a -> exit; }`
	workdir, runsdir, pipeline := setupRun(t, dot)
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r5"}); err != nil {
		t.Fatal(err)
	}
	types := readJSONLTypes(t, filepath.Join(runsdir, "r5", "events.jsonl"))
	count := 0
	for _, typ := range types {
		if typ == "StageRetrying" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 retries got %d", count)
	}
}

func TestRetryExhaustionFails(t *testing.T) {
	t.Setenv("ATTRACTION_BACKEND", "fake")
	dot := `digraph G { start [shape=Mdiamond]; a [shape=box, max_retries=1, "test.outcome"="retry"]; exit [shape=Msquare]; start -> a; a -> exit [condition="outcome=success"]; }`
	workdir, runsdir, pipeline := setupRun(t, dot)
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r6"}); err == nil {
		t.Fatal("expected failure")
	}
}

func TestRetryAllowPartial(t *testing.T) {
	t.Setenv("ATTRACTION_BACKEND", "fake")
	dot := `digraph G {
	start [shape=Mdiamond];
	a [shape=box, max_retries=1, allow_partial=true, "test.outcome"="retry"];
	exit [shape=Msquare];
	start -> a;
	a -> exit [condition="outcome=partial_success"];
	}`
	workdir, runsdir, pipeline := setupRun(t, dot)
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r7"}); err != nil {
		t.Fatal(err)
	}
}

func TestGuardWorkspaceCreatedAndUsed(t *testing.T) {
	dot := `digraph G { start [shape=Mdiamond]; t [shape=parallelogram, tool_command="cat seed.txt"]; exit [shape=Msquare]; start -> t; t -> exit; }`
	workdir, runsdir, pipeline := setupRun(t, dot)
	writeFile(t, filepath.Join(workdir, "seed.txt"), "hello")
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r8"}); err != nil {
		t.Fatal(err)
	}
	ob, _ := os.ReadFile(filepath.Join(runsdir, "r8", "t", "tool.stdout.txt"))
	if !strings.Contains(string(ob), "hello") {
		t.Fatal("expected seed content in stdout")
	}
	if _, err := os.Stat(filepath.Join(runsdir, "r8", "workspace", "seed.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceCopyExcludesNestedRunsDir(t *testing.T) {
	t.Setenv("ATTRACTION_BACKEND", "fake")
	dot := `digraph G { start [shape=Mdiamond]; a [shape=box]; exit [shape=Msquare]; start -> a; a -> exit; }`
	workdir, _, pipeline := setupRun(t, dot)
	runsdir := filepath.Join(workdir, ".runs")
	writeFile(t, filepath.Join(workdir, "seed.txt"), "hello")

	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "rin"}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(runsdir, "rin", "workspace", "seed.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runsdir, "rin", "workspace", ".runs")); err == nil {
		t.Fatal("workspace should not contain nested .runs copy")
	}
}

func TestGuardAllowlistPermitsSpecificWrites(t *testing.T) {
	dot := `digraph G { start [shape=Mdiamond]; t [shape=parallelogram, tool_command="sh -c 'echo hi > a.txt'", allowed_write_paths="a.txt"]; exit [shape=Msquare]; start -> t; t -> exit; }`
	workdir, runsdir, pipeline := setupRun(t, dot)
	writeFile(t, filepath.Join(workdir, "a.txt"), "x")
	writeFile(t, filepath.Join(workdir, "b.txt"), "y")
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r9"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(runsdir, "r9", "t", "workspace.diff.json"))
	if !strings.Contains(string(b), "a.txt") || strings.Contains(string(b), "b.txt") {
		t.Fatalf("unexpected diff: %s", string(b))
	}
}

func TestGuardAllowlistViolationFails(t *testing.T) {
	dot := `digraph G { start [shape=Mdiamond]; t [shape=parallelogram, tool_command="sh -c 'echo hi > b.txt'", allowed_write_paths="a.txt"]; exit [shape=Msquare]; start -> t; t -> exit; }`
	workdir, runsdir, pipeline := setupRun(t, dot)
	writeFile(t, filepath.Join(workdir, "a.txt"), "x")
	writeFile(t, filepath.Join(workdir, "b.txt"), "y")
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r10"}); err != nil {
		t.Fatal(err)
	}
	st, _ := os.ReadFile(filepath.Join(runsdir, "r10", "t", "status.json"))
	if !strings.Contains(string(st), "guardrail_violation") {
		t.Fatalf("expected guardrail reason: %s", string(st))
	}
	types := readJSONLTypes(t, filepath.Join(runsdir, "r10", "events.jsonl"))
	if !strings.Contains(strings.Join(types, ","), "GuardrailViolation") {
		t.Fatal("expected guardrail event")
	}
}

func TestGuardEscapeHeuristics(t *testing.T) {
	dot := `digraph G { start [shape=Mdiamond]; t [shape=parallelogram, tool_command="sh -c 'echo x > ../oops.txt'"]; exit [shape=Msquare]; start -> t; t -> exit; }`
	workdir, runsdir, pipeline := setupRun(t, dot)
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r11"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runsdir, "r11", "oops.txt")); err == nil {
		t.Fatal("oops.txt should not be created")
	}
}

func TestGuardAllowsGoTestDotDotDot(t *testing.T) {
	dot := `digraph G { start [shape=Mdiamond]; t [shape=parallelogram, tool_command="go test ./..."]; exit [shape=Msquare]; start -> t; t -> exit; }`
	workdir, runsdir, pipeline := setupRun(t, dot)
	writeFile(t, filepath.Join(workdir, "go.mod"), "module x\n\ngo 1.22\n")
	writeFile(t, filepath.Join(workdir, "x_test.go"), "package x\nimport \"testing\"\nfunc TestX(t *testing.T){}\n")
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r11b"}); err != nil {
		t.Fatal(err)
	}
	st, _ := os.ReadFile(filepath.Join(runsdir, "r11b", "t", "status.json"))
	if !strings.Contains(string(st), `"outcome": "success"`) {
		t.Fatalf("expected success, got: %s", string(st))
	}
}

func TestGuardNoWritesOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	workdir := filepath.Join(root, "work")
	runsdir := filepath.Join(root, "runs")
	pipeline := filepath.Join(root, "pipeline.dot")
	sentinel := filepath.Join(root, "sentinel.txt")
	writeFile(t, sentinel, "keep")
	writeFile(t, pipeline, `digraph G { start [shape=Mdiamond]; t [shape=parallelogram, tool_command="sh -c 'echo bad > /tmp/nope' "]; exit [shape=Msquare]; start -> t; t -> exit; }`)
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r12"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(sentinel)
	if string(b) != "keep" {
		t.Fatal("sentinel changed")
	}
}

func TestResumeDoesNotRerunCompletedNode(t *testing.T) {
	t.Setenv("ATTRACTION_BACKEND", "fake")
	t.Setenv("ATTRACTION_TEST_STOP_AFTER_NODE", "a")
	dot := `digraph G { start [shape=Mdiamond]; a [shape=box]; b [shape=box]; exit [shape=Msquare]; start -> a; a -> b; b -> exit; }`
	workdir, runsdir, pipeline := setupRun(t, dot)
	err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r13"})
	if err == nil || !strings.Contains(err.Error(), "test_stop") {
		t.Fatalf("expected test stop error got %v", err)
	}
	t.Setenv("ATTRACTION_TEST_STOP_AFTER_NODE", "")
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r13", Resume: true}); err != nil {
		t.Fatal(err)
	}
	types := readJSONLTypes(t, filepath.Join(runsdir, "r13", "events.jsonl"))
	seenB := false
	for _, typ := range types {
		if typ == "StageStarted" {
			seenB = true
			break
		}
	}
	if !seenB {
		t.Fatal("expected stage started events")
	}
}

func TestGuardAllowlistPermitsDirectoryWrites(t *testing.T) {
	dot := `digraph G {
	start [shape=Mdiamond];
	t [shape=parallelogram, tool_command="sh -c 'mkdir -p out && echo hi > out/a.txt'", allowed_write_paths="out/"];
	exit [shape=Msquare];
	start -> t;
	t -> exit;
	}`
	workdir, runsdir, pipeline := setupRun(t, dot)
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r14"}); err != nil {
		t.Fatal(err)
	}
	st, _ := os.ReadFile(filepath.Join(runsdir, "r14", "t", "status.json"))
	if strings.Contains(string(st), "guardrail_violation") {
		t.Fatalf("expected directory allowlist to pass: %s", string(st))
	}
}

func TestVerificationNodeFromPlanContext(t *testing.T) {
	t.Setenv("ATTRACTION_BACKEND", "fake")
	dot := `digraph G {
	start [shape=Mdiamond];
	generate [
		shape=box,
		"test.verification_plan_json"="{\"files\":[\"main.go\"],\"commands\":[\"test -f main.go\"]}"
	];
	verify [
		shape=parallelogram,
		type=verification,
		"verification.allowed_commands"="test -f"
	];
	exit [shape=Msquare];
	start -> generate;
	generate -> verify;
	verify -> exit [condition="outcome=success"];
	}`
	workdir, runsdir, pipeline := setupRun(t, dot)
	writeFile(t, filepath.Join(workdir, "main.go"), "package main\n")
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r15"}); err != nil {
		t.Fatal(err)
	}
	st, _ := os.ReadFile(filepath.Join(runsdir, "r15", "verify", "status.json"))
	if !strings.Contains(string(st), `"outcome": "success"`) {
		t.Fatalf("expected verification success: %s", string(st))
	}
	if _, err := os.Stat(filepath.Join(runsdir, "r15", "verify", "verification.plan.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runsdir, "r15", "verify", "verification.results.json")); err != nil {
		t.Fatal(err)
	}
}

func TestVerificationNodeRejectsCommandOutsideAllowlist(t *testing.T) {
	t.Setenv("ATTRACTION_BACKEND", "fake")
	dot := `digraph G {
	start [shape=Mdiamond];
	generate [
		shape=box,
		"test.verification_plan_json"="{\"files\":[\"main.go\"],\"commands\":[\"echo hi\"]}"
	];
	verify [
		shape=parallelogram,
		type=verification,
		"verification.allowed_commands"="go test"
	];
	exit [shape=Msquare];
	start -> generate;
	generate -> verify;
	verify -> exit [condition="outcome=success"];
	}`
	workdir, runsdir, pipeline := setupRun(t, dot)
	writeFile(t, filepath.Join(workdir, "main.go"), "package main\n")
	err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r16"})
	if err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(err.Error(), "no route from node verify for outcome fail") {
		t.Fatalf("unexpected error: %v", err)
	}
	st, _ := os.ReadFile(filepath.Join(runsdir, "r16", "verify", "status.json"))
	if !strings.Contains(string(st), "command not allowed") {
		t.Fatalf("expected command allowlist failure: %s", string(st))
	}
}

func TestVerificationNodeAllowsEnvPrefixedGoCommand(t *testing.T) {
	t.Setenv("ATTRACTION_BACKEND", "fake")
	dot := `digraph G {
	start [shape=Mdiamond];
	generate [
		shape=box,
		"test.verification_plan_json"="{\"files\":[\"go.mod\",\"main.go\"],\"commands\":[\"GOCACHE=\\\"$PWD/.gocache\\\" go test ./...\"]}"
	];
	verify [
		shape=parallelogram,
		type=verification,
		"verification.allowed_commands"="go test"
	];
	exit [shape=Msquare];
	start -> generate;
	generate -> verify;
	verify -> exit [condition="outcome=success"];
	}`
	workdir, runsdir, pipeline := setupRun(t, dot)
	writeFile(t, filepath.Join(workdir, "go.mod"), "module x\n\ngo 1.22\n")
	writeFile(t, filepath.Join(workdir, "main.go"), "package main\nfunc main(){}\n")
	writeFile(t, filepath.Join(workdir, "main_test.go"), "package main\nimport \"testing\"\nfunc TestX(t *testing.T){}\n")
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r17"}); err != nil {
		t.Fatal(err)
	}
	st, _ := os.ReadFile(filepath.Join(runsdir, "r17", "verify", "status.json"))
	if !strings.Contains(string(st), `"outcome": "success"`) {
		t.Fatalf("expected verification success: %s", string(st))
	}
}

func TestVerificationNodeUsesConfiguredWorkdir(t *testing.T) {
	t.Setenv("ATTRACTION_BACKEND", "fake")
	dot := `digraph G {
	start [shape=Mdiamond];
	generate [
		shape=box,
		"test.verification_plan_json"="{\"files\":[\"agent/main.go\",\"agent/main_test.go\"],\"commands\":[\"gofmt -w main.go main_test.go\"]}"
	];
	verify [
		shape=parallelogram,
		type=verification,
		"verification.allowed_commands"="gofmt",
		"verification.workdir"="agent"
	];
	exit [shape=Msquare];
	start -> generate;
	generate -> verify;
	verify -> exit [condition="outcome=success"];
	}`
	workdir, runsdir, pipeline := setupRun(t, dot)
	writeFile(t, filepath.Join(workdir, "agent/main.go"), "package main\nfunc main(){}\n")
	writeFile(t, filepath.Join(workdir, "agent/main_test.go"), "package main\nimport \"testing\"\nfunc TestX(t *testing.T){}\n")
	if err := RunPipeline(RunConfig{PipelinePath: pipeline, Workdir: workdir, Runsdir: runsdir, RunID: "r18"}); err != nil {
		t.Fatal(err)
	}
	st, _ := os.ReadFile(filepath.Join(runsdir, "r18", "verify", "status.json"))
	if !strings.Contains(string(st), `"outcome": "success"`) {
		t.Fatalf("expected verification success with configured workdir: %s", string(st))
	}
}

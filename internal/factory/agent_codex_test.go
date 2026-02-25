package attractor

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestHideAndRestoreWorkspacePaths(t *testing.T) {
	workspace := t.TempDir()
	nodeDir := filepath.Join(t.TempDir(), "node")
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scenariosDir := filepath.Join(workspace, "scripts", "scenarios")
	if err := os.MkdirAll(scenariosDir, 0o755); err != nil {
		t.Fatal(err)
	}
	origFile := filepath.Join(scenariosDir, "test.sh")
	if err := os.WriteFile(origFile, []byte("echo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hidden, err := hideWorkspacePaths(workspace, nodeDir, []string{"scripts/scenarios/"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(origFile); !os.IsNotExist(err) {
		t.Fatalf("expected file hidden, stat err=%v", err)
	}
	if len(hidden) != 1 {
		t.Fatalf("expected 1 hidden entry, got %d", len(hidden))
	}

	if err := restoreWorkspacePaths(hidden); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(origFile); err != nil {
		t.Fatalf("expected file restored: %v", err)
	}
}

func TestRestoreWorkspacePathsFailsWhenBlockedPathRecreated(t *testing.T) {
	workspace := t.TempDir()
	nodeDir := filepath.Join(t.TempDir(), "node")
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scenariosDir := filepath.Join(workspace, "scripts", "scenarios")
	if err := os.MkdirAll(scenariosDir, 0o755); err != nil {
		t.Fatal(err)
	}
	origFile := filepath.Join(scenariosDir, "test.sh")
	if err := os.WriteFile(origFile, []byte("echo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hidden, err := hideWorkspacePaths(workspace, nodeDir, []string{"scripts/scenarios/"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(scenariosDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(origFile, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := restoreWorkspacePaths(hidden); err == nil {
		t.Fatal("expected restore conflict error")
	}
}

func TestStrictReadScopeBlockedPaths(t *testing.T) {
	workspace := t.TempDir()
	mustMkdir := func(rel string) {
		if err := os.MkdirAll(filepath.Join(workspace, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite := func(rel string) {
		p := filepath.Join(workspace, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustMkdir("agent")
	mustMkdir("examples/specs")
	mustMkdir("scripts/scenarios")
	mustMkdir(".factory/bin")
	mustWrite("README.md")

	blocked, err := strictReadScopeBlockedPaths(
		workspace,
		filepath.Join(workspace, "agent"),
		[]string{filepath.Join(workspace, "examples/specs")},
		filepath.Join(workspace, ".factory/bin/codex"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(blocked, "agent/") {
		t.Fatalf("agent should not be blocked: %+v", blocked)
	}
	if slices.Contains(blocked, "examples/") {
		t.Fatalf("examples should not be blocked: %+v", blocked)
	}
	if slices.Contains(blocked, ".factory/") {
		t.Fatalf(".factory should not be blocked when executable is under it: %+v", blocked)
	}
	if !slices.Contains(blocked, "scripts/") {
		t.Fatalf("scripts should be blocked: %+v", blocked)
	}
	if !slices.Contains(blocked, "README.md") {
		t.Fatalf("README should be blocked: %+v", blocked)
	}
}

func TestCodexRunMissingExecutableHasClearError(t *testing.T) {
	workspace := t.TempDir()
	nodeDir := filepath.Join(t.TempDir(), "implement")
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(workspace, ".factory", "bin", "codex")
	agent := codexAgent{opts: CodexOptions{
		Executable:   missing,
		SandboxMode:  "workspace-write",
		Workdir:      workspace,
		DisableMCP:   true,
		TimeoutSeconds: 1,
	}}

	_, err := agent.Run(AgentRequest{
		Prompt:    "return success",
		NodeID:    "implement",
		NodeDir:   nodeDir,
		Workspace: workspace,
		Logger:    slog.Default(),
	})
	if err == nil {
		t.Fatal("expected missing executable error")
	}
	if !strings.Contains(err.Error(), "configured codex executable not found") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("expected missing path in error: %v", err)
	}
}

package attractor

import (
	"os"
	"path/filepath"
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

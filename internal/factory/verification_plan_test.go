package attractor

import (
	"path/filepath"
	"testing"
)

func TestParseVerificationPlanForWorkspace_AllowsAbsolutePathInsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	raw := map[string]any{
		"files": []string{filepath.Join(workspace, "agent", "main.go")},
		"commands": []string{
			"bash scripts/scenarios/agent_cli_component_checks.sh agent",
		},
	}

	plan, err := ParseVerificationPlanForWorkspace(raw, workspace)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Files) != 1 || plan.Files[0] != "agent/main.go" {
		t.Fatalf("unexpected normalized files: %+v", plan.Files)
	}
}

func TestParseVerificationPlanForWorkspace_RejectsAbsolutePathOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "agent", "main.go")
	raw := map[string]any{
		"files":    []string{outside},
		"commands": []string{"echo ok"},
	}

	if _, err := ParseVerificationPlanForWorkspace(raw, workspace); err == nil {
		t.Fatal("expected error for outside absolute path")
	}
}

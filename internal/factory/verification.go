package attractor

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type verificationHandler struct{}

type verificationCommandResult struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

type verificationResults struct {
	CheckedFiles []string                    `json:"checked_files"`
	Commands     []verificationCommandResult `json:"commands"`
}

func (verificationHandler) Execute(node *Node, ctx Context, _ *Graph, nodeDir string, workspace string) (Outcome, error) {
	key := strings.TrimSpace(node.StringAttr("verification.plan_context_key", "verification.plan"))
	raw, ok := ctx[key]
	if !ok {
		return Outcome{
			SchemaVersion:    1,
			Outcome:          "fail",
			SuggestedNextIDs: []string{},
			ContextUpdates:   map[string]any{},
			FailureReason:    fmt.Sprintf("verification plan missing in context key: %s", key),
		}, nil
	}
	plan, err := ParseVerificationPlan(raw)
	if err != nil {
		return Outcome{
			SchemaVersion:    1,
			Outcome:          "fail",
			SuggestedNextIDs: []string{},
			ContextUpdates:   map[string]any{},
			FailureReason:    err.Error(),
		}, nil
	}
	planJSON, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return Outcome{}, err
	}
	if err := os.WriteFile(filepath.Join(nodeDir, "verification.plan.json"), append(planJSON, '\n'), 0o644); err != nil {
		return Outcome{}, err
	}

	allowedPrefixes := splitCSV(node.StringAttr("verification.allowed_commands", ""))
	if len(allowedPrefixes) == 0 {
		return Outcome{
			SchemaVersion:    1,
			Outcome:          "fail",
			SuggestedNextIDs: []string{},
			ContextUpdates:   map[string]any{},
			FailureReason:    "verification.allowed_commands is required",
		}, nil
	}

	for _, f := range plan.Files {
		p := filepath.Join(workspace, filepath.FromSlash(f))
		if _, err := os.Stat(p); err != nil {
			return Outcome{
				SchemaVersion:    1,
				Outcome:          "fail",
				SuggestedNextIDs: []string{},
				ContextUpdates:   map[string]any{},
				FailureReason:    fmt.Sprintf("required file missing: %s", f),
			}, nil
		}
	}

	results := verificationResults{CheckedFiles: append([]string{}, plan.Files...), Commands: make([]verificationCommandResult, 0, len(plan.Commands))}
	for _, command := range plan.Commands {
		if err := validateToolCommand(command); err != nil {
			return Outcome{
				SchemaVersion:    1,
				Outcome:          "fail",
				SuggestedNextIDs: []string{},
				ContextUpdates:   map[string]any{},
				FailureReason:    err.Error(),
			}, nil
		}
		if !commandAllowed(command, allowedPrefixes) {
			return Outcome{
				SchemaVersion:    1,
				Outcome:          "fail",
				SuggestedNextIDs: []string{},
				ContextUpdates:   map[string]any{},
				FailureReason:    fmt.Sprintf("verification command not allowed: %s", command),
			}, nil
		}
		cmd := exec.Command("sh", "-c", command)
		cmd.Dir = workspace
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()
		if err := cmd.Start(); err != nil {
			return Outcome{}, err
		}
		outB, _ := io.ReadAll(stdout)
		errB, _ := io.ReadAll(stderr)
		err := cmd.Wait()
		exitCode := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				exitCode = ee.ExitCode()
			} else {
				return Outcome{}, err
			}
		}
		results.Commands = append(results.Commands, verificationCommandResult{
			Command:  command,
			ExitCode: exitCode,
			Stdout:   string(outB),
			Stderr:   string(errB),
		})
		if exitCode != 0 {
			b, _ := json.MarshalIndent(results, "", "  ")
			_ = os.WriteFile(filepath.Join(nodeDir, "verification.results.json"), append(b, '\n'), 0o644)
			return Outcome{
				SchemaVersion:    1,
				Outcome:          "fail",
				SuggestedNextIDs: []string{},
				ContextUpdates:   map[string]any{},
				FailureReason:    fmt.Sprintf("verification command failed: %s (exit=%d)", command, exitCode),
			}, nil
		}
	}

	b, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return Outcome{}, err
	}
	if err := os.WriteFile(filepath.Join(nodeDir, "verification.results.json"), append(b, '\n'), 0o644); err != nil {
		return Outcome{}, err
	}
	return Outcome{
		SchemaVersion:    1,
		Outcome:          "success",
		SuggestedNextIDs: []string{},
		ContextUpdates:   map[string]any{},
	}, nil
}

func commandAllowed(command string, allowedPrefixes []string) bool {
	cmd := strings.TrimSpace(command)
	for _, p := range allowedPrefixes {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if cmd == p {
			return true
		}
		if strings.HasPrefix(cmd, p+" ") {
			return true
		}
	}
	return false
}

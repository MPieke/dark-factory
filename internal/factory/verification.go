package attractor

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	plan, err := ParseVerificationPlanForWorkspace(raw, workspace)
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
	workingDir, err := resolveVerificationWorkdir(workspace, node.StringAttr("verification.workdir", ""))
	if err != nil {
		return Outcome{
			SchemaVersion:    1,
			Outcome:          "fail",
			SuggestedNextIDs: []string{},
			ContextUpdates:   map[string]any{},
			FailureReason:    err.Error(),
		}, nil
	}
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
		parsed, err := parseVerificationCommand(command, workingDir)
		if err != nil {
			return Outcome{
				SchemaVersion:    1,
				Outcome:          "fail",
				SuggestedNextIDs: []string{},
				ContextUpdates:   map[string]any{},
				FailureReason:    err.Error(),
			}, nil
		}
		cmd := exec.Command(parsed.Name, parsed.Args...)
		cmd.Dir = workingDir
		cmd.Env = append(os.Environ(), parsed.Env...)
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()
		if err := cmd.Start(); err != nil {
			return Outcome{}, err
		}
		outB, _ := io.ReadAll(stdout)
		errB, _ := io.ReadAll(stderr)
		waitErr := cmd.Wait()
		exitCode := 0
		if waitErr != nil {
			if ee, ok := waitErr.(*exec.ExitError); ok {
				exitCode = ee.ExitCode()
			} else {
				return Outcome{}, waitErr
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

func resolveVerificationWorkdir(workspace, configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return workspace, nil
	}
	if strings.HasPrefix(configured, "/") || filepath.IsAbs(configured) {
		return "", fmt.Errorf("verification.workdir must be relative")
	}
	clean := filepath.Clean(configured)
	for _, seg := range strings.Split(filepath.ToSlash(clean), "/") {
		if seg == ".." {
			return "", fmt.Errorf("verification.workdir cannot contain parent segment")
		}
	}
	dir := filepath.Join(workspace, clean)
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("verification.workdir missing: %s", configured)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("verification.workdir is not a directory: %s", configured)
	}
	return dir, nil
}

func commandAllowed(command string, allowedPrefixes []string) bool {
	if hasUnsafeShellSyntax(command) {
		return false
	}
	cmd := normalizeCommandForAllowlist(command)
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

type parsedVerificationCommand struct {
	Env  []string
	Name string
	Args []string
}

func parseVerificationCommand(command, workingDir string) (parsedVerificationCommand, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return parsedVerificationCommand{}, fmt.Errorf("verification command cannot be empty")
	}
	if hasUnsafeShellSyntax(command) {
		return parsedVerificationCommand{}, fmt.Errorf("verification command rejected: contains unsafe shell syntax")
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return parsedVerificationCommand{}, fmt.Errorf("verification command cannot be empty")
	}
	parsed := parsedVerificationCommand{}
	i := 0
	for i < len(fields) && isEnvAssignmentToken(fields[i]) {
		eq := strings.IndexByte(fields[i], '=')
		key := fields[i][:eq]
		val := expandVerificationEnvValue(fields[i][eq+1:], workingDir)
		parsed.Env = append(parsed.Env, key+"="+val)
		i++
	}
	if i >= len(fields) {
		return parsedVerificationCommand{}, fmt.Errorf("verification command missing executable")
	}
	parsed.Name = fields[i]
	parsed.Args = append([]string{}, fields[i+1:]...)
	return parsed, nil
}

func expandVerificationEnvValue(raw, workingDir string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 2 {
		if (raw[0] == '"' && raw[len(raw)-1] == '"') || (raw[0] == '\'' && raw[len(raw)-1] == '\'') {
			if unquoted, err := strconv.Unquote(raw); err == nil {
				raw = unquoted
			} else {
				raw = raw[1 : len(raw)-1]
			}
		}
	}
	raw = strings.ReplaceAll(raw, "$PWD", workingDir)
	raw = strings.ReplaceAll(raw, "${PWD}", workingDir)
	return raw
}

func hasUnsafeShellSyntax(command string) bool {
	command = strings.TrimSpace(command)
	unsafe := []string{"&&", "||", ";", "|", "`", "$(", ">", "<", "\n", "\r"}
	for _, token := range unsafe {
		if strings.Contains(command, token) {
			return true
		}
	}
	return false
}

func normalizeCommandForAllowlist(command string) string {
	cmd := strings.TrimSpace(command)
	for {
		original := cmd
		cmd = trimWrappingParens(cmd)
		cmd = stripLeadingEnvAssignments(cmd)
		cmd = stripLeadingShellWrappers(cmd)
		cmd = strings.TrimSpace(cmd)
		if cmd == original {
			break
		}
	}
	return cmd
}

func trimWrappingParens(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	for len(cmd) >= 2 && strings.HasPrefix(cmd, "(") && strings.HasSuffix(cmd, ")") {
		inner := strings.TrimSpace(cmd[1 : len(cmd)-1])
		if inner == "" {
			break
		}
		cmd = inner
	}
	return cmd
}

func stripLeadingEnvAssignments(cmd string) string {
	fields := strings.Fields(cmd)
	i := 0
	for i < len(fields) && isEnvAssignmentToken(fields[i]) {
		i++
	}
	if i == 0 {
		return cmd
	}
	return strings.Join(fields[i:], " ")
}

func isEnvAssignmentToken(tok string) bool {
	if tok == "" || strings.HasPrefix(tok, "=") || strings.HasSuffix(tok, "=") {
		return false
	}
	eq := strings.IndexByte(tok, '=')
	if eq <= 0 {
		return false
	}
	key := tok[:eq]
	for i, r := range key {
		if i == 0 {
			if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && r != '_' {
				return false
			}
			continue
		}
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func stripLeadingShellWrappers(cmd string) string {
	trimmed := strings.TrimSpace(cmd)
	if strings.HasPrefix(trimmed, "export ") {
		if idx := strings.Index(trimmed, "&&"); idx >= 0 {
			return strings.TrimSpace(trimmed[idx+2:])
		}
	}
	if strings.HasPrefix(trimmed, "cd ") {
		if idx := strings.Index(trimmed, "&&"); idx >= 0 {
			return strings.TrimSpace(trimmed[idx+2:])
		}
	}
	return cmd
}

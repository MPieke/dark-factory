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

type codexAgent struct {
	opts CodexOptions
}

func (a codexAgent) Run(req AgentRequest) (AgentResponse, error) {
	schemaPath := filepath.Join(req.NodeDir, "codex.output.schema.json")
	outputPath := filepath.Join(req.NodeDir, "response.md")
	stdoutPath := filepath.Join(req.NodeDir, "codex.stdout.log")
	stderrPath := filepath.Join(req.NodeDir, "codex.stderr.log")

	if err := os.WriteFile(schemaPath, []byte(codexOutcomeSchema+"\n"), 0o644); err != nil {
		return AgentResponse{}, err
	}
	args, err := buildCodexExecArgs(a.opts, schemaPath, outputPath)
	if err != nil {
		return AgentResponse{}, err
	}

	cmd := exec.Command("codex", args...)
	cmd.Stdin = strings.NewReader(req.Prompt + "\n\nReturn only JSON matching the provided schema.")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return AgentResponse{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return AgentResponse{}, err
	}
	if err := cmd.Start(); err != nil {
		return AgentResponse{}, err
	}
	outB, _ := io.ReadAll(stdout)
	errB, _ := io.ReadAll(stderr)
	runErr := cmd.Wait()

	_ = os.WriteFile(stdoutPath, outB, 0o644)
	_ = os.WriteFile(stderrPath, errB, 0o644)
	if runErr != nil {
		return AgentResponse{}, fmt.Errorf("codex exec failed: %w", runErr)
	}

	raw, err := os.ReadFile(outputPath)
	if err != nil {
		return AgentResponse{}, fmt.Errorf("codex output missing: %w", err)
	}
	parsed := AgentResponse{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return AgentResponse{}, fmt.Errorf("codex output is not valid JSON: %w", err)
	}
	if parsed.Outcome == "" {
		return AgentResponse{}, fmt.Errorf("codex output missing outcome")
	}
	if parsed.ContextUpdates == nil {
		parsed.ContextUpdates = map[string]any{}
	}
	return parsed, nil
}

func buildCodexExecArgs(opts CodexOptions, schemaPath, outputPath string) ([]string, error) {
	args := []string{}
	if opts.ApprovalPolicy != "" {
		args = append(args, "-a", opts.ApprovalPolicy)
	}
	args = append(args, "exec")
	if opts.SandboxMode != "" {
		args = append(args, "-s", opts.SandboxMode)
	}
	if opts.Model != "" {
		args = append(args, "-m", opts.Model)
	}
	if opts.Profile != "" {
		args = append(args, "-p", opts.Profile)
	}
	for _, override := range opts.ConfigOverrides {
		args = append(args, "-c", override)
	}
	if len(opts.AutoApproveCommands) > 0 {
		if strings.TrimSpace(opts.AutoApproveConfigKey) == "" {
			return nil, fmt.Errorf("codex.auto_approve_commands requires codex.auto_approve_config_key")
		}
		override := fmt.Sprintf("%s=%s", opts.AutoApproveConfigKey, toTOMLArray(opts.AutoApproveCommands))
		args = append(args, "-c", override)
	}
	if opts.Workdir != "" {
		args = append(args, "-C", opts.Workdir)
	}
	if opts.SkipGitRepoCheck {
		args = append(args, "--skip-git-repo-check")
	}
	for _, d := range opts.AddDirs {
		args = append(args, "--add-dir", d)
	}
	if opts.DangerousBypass {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	}
	args = append(args, "--color", "never", "--output-schema", schemaPath, "-o", outputPath, "-")
	return args, nil
}

func toTOMLArray(values []string) string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		v = strings.ReplaceAll(v, `\`, `\\`)
		v = strings.ReplaceAll(v, `"`, `\"`)
		out = append(out, `"`+v+`"`)
	}
	return "[" + strings.Join(out, ",") + "]"
}

const codexOutcomeSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "AttractorCodexOutcome",
  "type": "object",
  "additionalProperties": false,
  "required": ["outcome", "preferred_next_label", "suggested_next_ids", "context_updates", "verification_plan", "notes", "failure_reason"],
  "properties": {
    "outcome": {
      "type": "string",
      "enum": ["success", "fail", "retry", "partial_success"]
    },
    "preferred_next_label": {
      "type": "string"
    },
    "suggested_next_ids": {
      "type": "array",
      "items": { "type": "string" }
    },
    "context_updates": {
      "type": "object",
      "properties": {},
      "additionalProperties": false
    },
    "verification_plan": {
      "anyOf": [
        { "type": "null" },
        {
          "type": "object",
          "additionalProperties": false,
          "required": ["files", "commands"],
          "properties": {
            "files": {
              "type": "array",
              "items": { "type": "string" }
            },
            "commands": {
              "type": "array",
              "items": { "type": "string" }
            }
          }
        }
      ]
    },
    "notes": {
      "type": "string"
    },
    "failure_reason": {
      "type": "string"
    }
  }
}`

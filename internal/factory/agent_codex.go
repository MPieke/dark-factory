package attractor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type codexAgent struct {
	opts CodexOptions
}

func (a codexAgent) Run(req AgentRequest) (AgentResponse, error) {
	logger := req.Logger
	if logger == nil {
		logger = slog.Default()
	}
	schemaPath := filepath.Join(req.NodeDir, "codex.output.schema.json")
	outputPath := filepath.Join(req.NodeDir, "response.md")
	stdoutPath := filepath.Join(req.NodeDir, "codex.stdout.log")
	stderrPath := filepath.Join(req.NodeDir, "codex.stderr.log")
	argsPath := filepath.Join(req.NodeDir, "codex.args.txt")

	if err := os.WriteFile(schemaPath, []byte(codexOutcomeSchema+"\n"), 0o644); err != nil {
		return AgentResponse{}, err
	}
	args, err := buildCodexExecArgs(a.opts, schemaPath, outputPath)
	if err != nil {
		return AgentResponse{}, err
	}

	if err := os.WriteFile(argsPath, []byte(strings.Join(append([]string{"codex"}, args...), " ")+"\n"), 0o644); err != nil {
		return AgentResponse{}, err
	}

	ctx := context.Background()
	cancel := func() {}
	if a.opts.TimeoutSeconds > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(a.opts.TimeoutSeconds)*time.Second)
	}
	defer cancel()

	cmd := exec.CommandContext(ctx, "codex", args...)
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
	stdoutFile, err := os.Create(stdoutPath)
	if err != nil {
		return AgentResponse{}, err
	}
	defer stdoutFile.Close()
	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		return AgentResponse{}, err
	}
	defer stderrFile.Close()
	logger.Info("codex exec started",
		"node", req.NodeID,
		"workdir", a.opts.Workdir,
		"model", a.opts.Model,
		"sandbox", a.opts.SandboxMode,
		"approval", a.opts.ApprovalPolicy,
		"timeout_seconds", a.opts.TimeoutSeconds,
		"args_path", argsPath,
		"stdout_log", stdoutPath,
		"stderr_log", stderrPath,
	)

	heartbeatDone := make(chan struct{})
	heartbeatSeconds := a.opts.HeartbeatSeconds
	if heartbeatSeconds <= 0 {
		heartbeatSeconds = 15
	}
	go func() {
		t := time.NewTicker(time.Duration(heartbeatSeconds) * time.Second)
		defer t.Stop()
		for {
			select {
			case <-heartbeatDone:
				return
			case <-t.C:
				logger.Info("codex exec still running", "node", req.NodeID, "heartbeat_seconds", heartbeatSeconds)
			}
		}
	}()

	logStream := parseBool("FACTORY_LOG_CODEX_STREAM", false)
	var outErr error
	var errErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		outErr = readAndMaybeLogStream(stdout, stdoutFile, "stdout", req.NodeID, logger, logStream)
	}()
	go func() {
		defer wg.Done()
		errErr = readAndMaybeLogStream(stderr, stderrFile, "stderr", req.NodeID, logger, logStream)
	}()
	runErr := cmd.Wait()
	wg.Wait()
	close(heartbeatDone)
	if outErr != nil {
		return AgentResponse{}, fmt.Errorf("failed reading codex stdout: %w", outErr)
	}
	if errErr != nil {
		return AgentResponse{}, fmt.Errorf("failed reading codex stderr: %w", errErr)
	}
	if runErr != nil {
		if ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			logger.Error("codex exec timed out", "node", req.NodeID, "timeout_seconds", a.opts.TimeoutSeconds)
			return AgentResponse{}, fmt.Errorf("codex exec timeout after %ds", a.opts.TimeoutSeconds)
		}
		logger.Error("codex exec failed", "node", req.NodeID, "error", runErr, "stderr_log", stderrPath, "stdout_log", stdoutPath)
		return AgentResponse{}, fmt.Errorf("codex exec failed: %w", runErr)
	}
	logger.Info("codex exec completed", "node", req.NodeID, "stdout_log", stdoutPath, "stderr_log", stderrPath, "response_path", outputPath)

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

func readAndMaybeLogStream(r io.Reader, sink io.Writer, stream string, nodeID string, logger *slog.Logger, logStream bool) error {
	buf := make([]byte, 4096)
	var pending string
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			if _, werr := sink.Write(buf[:n]); werr != nil {
				return werr
			}
			if logStream {
				pending += chunk
				lines, rest := splitLogRecords(pending)
				pending = rest
				for _, line := range lines {
					if strings.TrimSpace(line) == "" {
						continue
					}
					logger.Info("codex stream", "node", nodeID, "stream", stream, "line", line)
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
	}
	if logStream && strings.TrimSpace(pending) != "" {
		logger.Info("codex stream", "node", nodeID, "stream", stream, "line", strings.TrimSpace(pending))
	}
	return nil
}

func parseBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func splitLogRecords(s string) ([]string, string) {
	lines := make([]string, 0, 8)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			if i > start {
				lines = append(lines, s[start:i])
			}
			start = i + 1
		}
	}
	if start >= len(s) {
		return lines, ""
	}
	return lines, s[start:]
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

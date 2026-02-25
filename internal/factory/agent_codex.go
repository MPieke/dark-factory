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
	"sort"
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
	hiddenPaths, err := hideWorkspacePaths(req.Workspace, req.NodeDir, a.opts.BlockReadPaths)
	if err != nil {
		return AgentResponse{}, err
	}
	if a.opts.StrictReadScope {
		scopeBlocked, err := strictReadScopeBlockedPaths(req.Workspace, a.opts.Workdir, a.opts.AddDirs, a.opts.Executable)
		if err != nil {
			_ = restoreWorkspacePaths(hiddenPaths)
			return AgentResponse{}, err
		}
		if len(scopeBlocked) > 0 {
			additionalHidden, err := hideWorkspacePaths(req.Workspace, req.NodeDir, scopeBlocked)
			if err != nil {
				_ = restoreWorkspacePaths(hiddenPaths)
				return AgentResponse{}, err
			}
			hiddenPaths = append(hiddenPaths, additionalHidden...)
		}
	}
	defer func() {
		if restoreErr := restoreWorkspacePaths(hiddenPaths); restoreErr != nil {
			logger.Error("failed to restore hidden paths", "node", req.NodeID, "error", restoreErr)
		}
	}()

	ctx := context.Background()
	cancel := func() {}
	if a.opts.TimeoutSeconds > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(a.opts.TimeoutSeconds)*time.Second)
	}
	defer cancel()

	if err := validateConfiguredExecutable(a.opts.Executable); err != nil {
		return AgentResponse{}, err
	}

	cmd := exec.CommandContext(ctx, a.opts.Executable, args...)
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
		"executable", a.opts.Executable,
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

func validateConfiguredExecutable(executable string) error {
	if strings.TrimSpace(executable) == "" {
		return nil
	}
	if !strings.Contains(executable, "/") {
		return nil
	}
	info, err := os.Stat(executable)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf(
				"configured codex executable not found: %s (set codex.path to an existing executable or create it before running)",
				executable,
			)
		}
		return fmt.Errorf("failed to stat configured codex executable %s: %w", executable, err)
	}
	if info.IsDir() {
		return fmt.Errorf("configured codex executable is a directory: %s", executable)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("configured codex executable is not executable: %s", executable)
	}
	return nil
}

type hiddenPath struct {
	original string
	hidden   string
}

func hideWorkspacePaths(workspace, nodeDir string, blocked []string) ([]hiddenPath, error) {
	if len(blocked) == 0 {
		return nil, nil
	}
	base, err := os.MkdirTemp(nodeDir, ".hidden_read_paths.")
	if err != nil {
		return nil, err
	}
	paths := append([]string(nil), blocked...)
	sort.Strings(paths)
	hidden := make([]hiddenPath, 0, len(paths))
	for _, rel := range paths {
		rel = filepath.ToSlash(strings.TrimSpace(rel))
		if rel == "" {
			continue
		}
		if strings.HasPrefix(rel, "/") {
			return nil, fmt.Errorf("blocked read path %q must be relative", rel)
		}
		for _, seg := range strings.Split(filepath.Clean(rel), string(filepath.Separator)) {
			if seg == ".." {
				return nil, fmt.Errorf("blocked read path %q contains parent segment", rel)
			}
		}
		orig := filepath.Join(workspace, filepath.FromSlash(rel))
		if _, err := os.Stat(orig); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		dst := filepath.Join(base, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, err
		}
		if err := os.Rename(orig, dst); err != nil {
			_ = restoreWorkspacePaths(hidden)
			return nil, err
		}
		hidden = append(hidden, hiddenPath{original: orig, hidden: dst})
	}
	return hidden, nil
}

func restoreWorkspacePaths(hidden []hiddenPath) error {
	if len(hidden) == 0 {
		return nil
	}
	for i := len(hidden) - 1; i >= 0; i-- {
		h := hidden[i]
		if err := os.MkdirAll(filepath.Dir(h.original), 0o755); err != nil {
			return err
		}
		if _, err := os.Stat(h.original); err == nil {
			return fmt.Errorf("blocked path was recreated during execution: %s", h.original)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(h.hidden, h.original); err != nil {
			return err
		}
	}
	return nil
}

func strictReadScopeBlockedPaths(workspace, workdir string, addDirs []string, executable string) ([]string, error) {
	keepRoots := map[string]bool{}
	addKeepRoot := func(abs string) {
		rel, err := filepath.Rel(workspace, abs)
		if err != nil {
			return
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		if rel == "." || strings.HasPrefix(rel, "../") {
			return
		}
		root := strings.Split(rel, "/")[0]
		if root != "" && root != "." {
			keepRoots[root] = true
		}
	}
	addKeepRoot(workdir)
	for _, d := range addDirs {
		addKeepRoot(d)
	}
	addKeepRoot(executable)
	if len(keepRoots) == 0 {
		return nil, nil
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return nil, err
	}
	blocked := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if keepRoots[name] {
			continue
		}
		if entry.IsDir() {
			blocked = append(blocked, name+"/")
			continue
		}
		blocked = append(blocked, name)
	}
	sort.Strings(blocked)
	return blocked, nil
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
	if opts.DisableMCP && !containsConfigOverride(opts.ConfigOverrides, "mcp_servers.memory_ops.enabled") {
		args = append(args, "-c", "mcp_servers.memory_ops.enabled=false")
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

func containsConfigOverride(overrides []string, key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	prefix := key + "="
	for _, o := range overrides {
		o = strings.TrimSpace(o)
		if strings.HasPrefix(o, prefix) {
			return true
		}
	}
	return false
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

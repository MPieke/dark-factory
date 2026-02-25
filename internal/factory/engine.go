package attractor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Outcome struct {
	SchemaVersion      int            `json:"schema_version"`
	Outcome            string         `json:"outcome"`
	PreferredNextLabel string         `json:"preferred_next_label"`
	SuggestedNextIDs   []string       `json:"suggested_next_ids"`
	ContextUpdates     map[string]any `json:"context_updates"`
	Notes              string         `json:"notes"`
	FailureReason      string         `json:"failure_reason"`
}

type Checkpoint struct {
	SchemaVersion     int            `json:"schema_version"`
	RunID             string         `json:"run_id"`
	LastCompletedNode string         `json:"last_completed_node"`
	CompletedNodes    []string       `json:"completed_nodes"`
	RetryCounts       map[string]int `json:"retry_counts"`
	Context           map[string]any `json:"context"`
}

type fileState struct {
	Size int64
	Hash string
}

type workspaceDiff struct {
	Created  []string `json:"created"`
	Modified []string `json:"modified"`
	Deleted  []string `json:"deleted"`
}

type RunConfig struct {
	PipelinePath string
	Workdir      string
	Runsdir      string
	RunID        string
	Resume       bool
}

type Handler interface {
	Execute(node *Node, ctx Context, g *Graph, nodeDir string, workspace string) (Outcome, error)
}

type Engine struct {
	Graph      *Graph
	RunID      string
	RunDir     string
	Workspace  string
	Context    Context
	RetryCount map[string]int
	Completed  map[string]bool
	Logger     *slog.Logger
}

func RunPipeline(cfg RunConfig) error {
	logger := newFactoryLogger()
	slog.SetDefault(logger)
	logger.Info("pipeline starting", "pipeline_path", cfg.PipelinePath, "workdir", cfg.Workdir, "runsdir", cfg.Runsdir, "resume", cfg.Resume)
	b, err := os.ReadFile(cfg.PipelinePath)
	if err != nil {
		logger.Error("failed to read pipeline", "error", err)
		return err
	}
	g, err := ParseDOT(string(b))
	if err != nil {
		logger.Error("failed to parse pipeline", "error", err)
		return err
	}
	diags := ValidateGraph(g)
	if HasErrors(diags) {
		msgs := []string{}
		for _, d := range diags {
			if d.Level == "ERROR" {
				msgs = append(msgs, d.Message)
			}
		}
		logger.Error("pipeline validation failed", "errors", strings.Join(msgs, "; "))
		return fmt.Errorf("validation failed: %s", strings.Join(msgs, "; "))
	}

	if cfg.Resume {
		if cfg.RunID == "" {
			logger.Error("resume requested without run-id")
			return fmt.Errorf("--run-id required with --resume")
		}
	} else if cfg.RunID == "" {
		cfg.RunID = time.Now().UTC().Format("20060102_150405")
	}
	runDir := filepath.Join(cfg.Runsdir, cfg.RunID)
	workspace := filepath.Join(runDir, "workspace")

	if cfg.Resume {
	} else {
		if err := os.MkdirAll(workspace, 0o755); err != nil {
			logger.Error("failed to create workspace", "workspace", workspace, "error", err)
			return err
		}
		excludes := []string{".git"}
		if relRuns, ok := relativeDescendant(cfg.Workdir, cfg.Runsdir); ok {
			excludes = append(excludes, relRuns)
			logger.Info("excluding runsdir from workspace copy", "relative_path", relRuns)
		}
		if err := copyDir(cfg.Workdir, workspace, excludes); err != nil {
			logger.Error("failed to copy workdir into workspace", "error", err)
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(workspace, ".attractor"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	if err := writeManifest(g, cfg, runDir, workspace); err != nil {
		logger.Error("failed to write manifest", "error", err)
		return err
	}
	_ = appendTrace(runDir, "SessionInitialized", map[string]any{
		"run_id":        cfg.RunID,
		"pipeline_path": cfg.PipelinePath,
		"workdir":       cfg.Workdir,
		"workspace":     workspace,
		"resume":        cfg.Resume,
	})

	e := &Engine{Graph: g, RunID: cfg.RunID, RunDir: runDir, Workspace: workspace, Context: Context{}, RetryCount: map[string]int{}, Completed: map[string]bool{}, Logger: logger}
	if goal, ok := g.Attrs["goal"]; ok {
		e.Context["graph.goal"] = goal
	}
	startID := findStartNode(g).ID
	if cfg.Resume {
		cp, err := readCheckpoint(filepath.Join(runDir, "checkpoint.json"))
		if err != nil {
			return err
		}
		e.Context = Context(cp.Context)
		e.RetryCount = cp.RetryCounts
		for _, id := range cp.CompletedNodes {
			e.Completed[id] = true
		}
		if cp.LastCompletedNode != "" {
			status, err := readStatus(filepath.Join(runDir, cp.LastCompletedNode, "status.json"))
			if err != nil {
				return err
			}
			_ = appendTrace(runDir, "ResumeLoaded", map[string]any{
				"last_completed_node": cp.LastCompletedNode,
				"last_outcome":        status.Outcome,
				"completed_nodes":     cp.CompletedNodes,
			})
			next := e.selectNext(cp.LastCompletedNode, status.Outcome)
			if next == "" {
				if isExit(g, cp.LastCompletedNode) {
					return nil
				}
				return fmt.Errorf("resume failed: no route from %s", cp.LastCompletedNode)
			}
			startID = next
		}
	}

	_ = appendEvent(runDir, map[string]any{"schema_version": 1, "type": "PipelineStarted", "run_id": cfg.RunID, "at": time.Now().UTC().Format(time.RFC3339Nano)})
	_ = appendTrace(runDir, "PipelineStarted", map[string]any{"run_id": cfg.RunID, "start_node": startID})
	logger.Info("pipeline execution started", "run_id", cfg.RunID, "run_dir", runDir, "workspace", workspace, "start_node", startID)
	if err := e.executeFrom(startID); err != nil {
		_ = appendEvent(runDir, map[string]any{"schema_version": 1, "type": "PipelineFailed", "error": err.Error(), "at": time.Now().UTC().Format(time.RFC3339Nano)})
		_ = appendTrace(runDir, "PipelineFailed", map[string]any{"error": err.Error()})
		logger.Error("pipeline failed", "run_id", cfg.RunID, "error", err)
		return err
	}
	_ = appendEvent(runDir, map[string]any{"schema_version": 1, "type": "PipelineCompleted", "at": time.Now().UTC().Format(time.RFC3339Nano)})
	_ = appendTrace(runDir, "PipelineCompleted", map[string]any{})
	logger.Info("pipeline completed", "run_id", cfg.RunID)
	return nil
}

func (e *Engine) executeFrom(startID string) error {
	current := startID
	for {
		node := e.Graph.Nodes[current]
		if node == nil {
			return fmt.Errorf("missing node: %s", current)
		}
		nodeDir := filepath.Join(e.RunDir, node.ID)
		if err := os.MkdirAll(nodeDir, 0o755); err != nil {
			return err
		}
		_ = appendEvent(e.RunDir, map[string]any{"schema_version": 1, "type": "StageStarted", "node_id": node.ID, "at": time.Now().UTC().Format(time.RFC3339Nano)})
		e.Logger.Info("stage started", "node", node.ID, "type", node.Type(), "shape", node.Shape())
		contextBefore := cloneContext(e.Context)
		_ = appendTrace(e.RunDir, "NodeInputCaptured", map[string]any{
			"node_id":           node.ID,
			"node_type":         node.Type(),
			"node_shape":        node.Shape(),
			"node_attrs":        cloneMap(node.Attrs),
			"context_before":    contextBefore,
			"workspace":         e.Workspace,
			"node_artifact_dir": nodeDir,
		})
		e.Context["current_node"] = node.ID
		out, err := e.executeNode(node, nodeDir)
		if err != nil {
			_ = appendEvent(e.RunDir, map[string]any{"schema_version": 1, "type": "StageFailed", "node_id": node.ID, "error": err.Error(), "at": time.Now().UTC().Format(time.RFC3339Nano)})
			_ = appendTrace(e.RunDir, "NodeExecutionErrored", map[string]any{"node_id": node.ID, "error": err.Error()})
			e.Logger.Error("stage execution errored", "node", node.ID, "error", err)
			e.logFailureContext(node, nodeDir)
			return err
		}
		if err := writeJSON(filepath.Join(nodeDir, "status.json"), out); err != nil {
			return err
		}
		if out.Outcome == "fail" {
			_ = appendEvent(e.RunDir, map[string]any{"schema_version": 1, "type": "StageFailed", "node_id": node.ID, "failure_reason": out.FailureReason, "at": time.Now().UTC().Format(time.RFC3339Nano)})
			e.Logger.Warn("stage failed", "node", node.ID, "reason", out.FailureReason)
			e.logFailureContext(node, nodeDir)
		} else {
			_ = appendEvent(e.RunDir, map[string]any{"schema_version": 1, "type": "StageCompleted", "node_id": node.ID, "outcome": out.Outcome, "at": time.Now().UTC().Format(time.RFC3339Nano)})
			e.Logger.Info("stage completed", "node", node.ID, "outcome", out.Outcome)
		}
		for k, v := range out.ContextUpdates {
			e.Context[k] = v
		}
		if out.Outcome == "fail" {
			e.captureFailureFeedback(node, nodeDir, out)
		}
		e.Context["outcome"] = out.Outcome
		contextAfter := cloneContext(e.Context)
		_ = appendTrace(e.RunDir, "NodeOutputCaptured", map[string]any{
			"node_id":         node.ID,
			"outcome":         out.Outcome,
			"failure_reason":  out.FailureReason,
			"context_updates": cloneMap(out.ContextUpdates),
			"context_after":   contextAfter,
			"context_delta":   computeContextDelta(contextBefore, contextAfter),
			"status_path":     filepath.Join(node.ID, "status.json"),
		})
		e.Completed[node.ID] = true
		if err := e.writeCheckpoint(node.ID); err != nil {
			return err
		}
		if stop := os.Getenv("ATTRACTION_TEST_STOP_AFTER_NODE"); stop != "" && stop == node.ID {
			return errors.New("test_stop")
		}
		if isExit(e.Graph, node.ID) {
			return nil
		}
		next := e.selectNext(node.ID, out.Outcome)
		_ = appendTrace(e.RunDir, "RouteEvaluated", map[string]any{
			"from_node":  node.ID,
			"outcome":    out.Outcome,
			"next_node":  next,
			"candidates": routeCandidates(e.Graph, node.ID, out.Outcome),
		})
		e.Logger.Info("route selected", "from_node", node.ID, "outcome", out.Outcome, "next_node", next)
		if next == "" {
			return fmt.Errorf("no route from node %s for outcome %s", node.ID, out.Outcome)
		}
		current = next
	}
}

func (e *Engine) logFailureContext(node *Node, nodeDir string) {
	paths := map[string]string{
		"status_path":            filepath.Join(nodeDir, "status.json"),
		"tool_stdout_path":       filepath.Join(nodeDir, "tool.stdout.txt"),
		"tool_stderr_path":       filepath.Join(nodeDir, "tool.stderr.txt"),
		"tool_exitcode_path":     filepath.Join(nodeDir, "tool.exitcode.txt"),
		"verification_results":   filepath.Join(nodeDir, "verification.results.json"),
		"verification_plan_path": filepath.Join(nodeDir, "verification.plan.json"),
		"codex_stdout_path":      filepath.Join(nodeDir, "codex.stdout.log"),
		"codex_stderr_path":      filepath.Join(nodeDir, "codex.stderr.log"),
		"codex_response_path":    filepath.Join(nodeDir, "response.md"),
		"workspace_diff_path":    filepath.Join(nodeDir, "workspace.diff.json"),
	}
	attrs := []any{"node", node.ID}
	for key, p := range paths {
		if _, err := os.Stat(p); err == nil {
			attrs = append(attrs, key, p)
		}
	}
	e.Logger.Warn("failure artifacts", attrs...)

	if tail, ok := readTailSnippet(filepath.Join(nodeDir, "tool.stderr.txt"), 600); ok {
		e.Logger.Warn("failure detail", "node", node.ID, "source", "tool.stderr.txt", "snippet", tail)
	}
	if tail, ok := readTailSnippet(filepath.Join(nodeDir, "tool.stdout.txt"), 600); ok {
		e.Logger.Warn("failure detail", "node", node.ID, "source", "tool.stdout.txt", "snippet", tail)
	}
	if tail, ok := readTailSnippet(filepath.Join(nodeDir, "codex.stderr.log"), 600); ok {
		e.Logger.Warn("failure detail", "node", node.ID, "source", "codex.stderr.log", "snippet", tail)
	}
	if tail, ok := readTailSnippet(filepath.Join(nodeDir, "response.md"), 600); ok {
		e.Logger.Warn("failure detail", "node", node.ID, "source", "response.md", "snippet", tail)
	}
}

func (e *Engine) captureFailureFeedback(node *Node, nodeDir string, out Outcome) {
	artifacts := map[string]string{}
	candidates := map[string]string{
		"status":               filepath.Join(nodeDir, "status.json"),
		"tool_stdout":          filepath.Join(nodeDir, "tool.stdout.txt"),
		"tool_stderr":          filepath.Join(nodeDir, "tool.stderr.txt"),
		"tool_exitcode":        filepath.Join(nodeDir, "tool.exitcode.txt"),
		"verification_results": filepath.Join(nodeDir, "verification.results.json"),
		"verification_plan":    filepath.Join(nodeDir, "verification.plan.json"),
		"codex_stdout":         filepath.Join(nodeDir, "codex.stdout.log"),
		"codex_stderr":         filepath.Join(nodeDir, "codex.stderr.log"),
		"codex_response":       filepath.Join(nodeDir, "response.md"),
	}
	for key, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			artifacts[key] = p
		}
	}
	e.Context["last_failure.node_id"] = node.ID
	e.Context["last_failure.node_type"] = node.Type()
	e.Context["last_failure.reason"] = out.FailureReason
	e.Context["last_failure.at"] = time.Now().UTC().Format(time.RFC3339Nano)
	e.Context["last_failure.artifacts"] = artifacts
	e.Context["last_failure.summary"] = buildFailureSummary(node, nodeDir, out)
}

func buildFailureSummary(node *Node, nodeDir string, out Outcome) string {
	parts := []string{
		fmt.Sprintf("failed_node=%s", node.ID),
		fmt.Sprintf("failed_node_type=%s", node.Type()),
	}
	if strings.TrimSpace(out.FailureReason) != "" {
		parts = append(parts, fmt.Sprintf("failure_reason=%s", out.FailureReason))
	}
	if code, ok := readTailSnippet(filepath.Join(nodeDir, "tool.exitcode.txt"), 64); ok {
		parts = append(parts, fmt.Sprintf("tool_exit_code=%s", strings.TrimSpace(code)))
	}
	if s, ok := readTailSnippet(filepath.Join(nodeDir, "tool.stderr.txt"), 600); ok {
		parts = append(parts, "tool_stderr:\n"+s)
	}
	if s, ok := readTailSnippet(filepath.Join(nodeDir, "tool.stdout.txt"), 300); ok {
		parts = append(parts, "tool_stdout:\n"+s)
	}
	if s, ok := readTailSnippet(filepath.Join(nodeDir, "verification.results.json"), 600); ok {
		parts = append(parts, "verification_results_tail:\n"+s)
	}
	if s, ok := readTailSnippet(filepath.Join(nodeDir, "codex.stderr.log"), 600); ok {
		parts = append(parts, "codex_stderr_tail:\n"+s)
	}
	summary := strings.Join(parts, "\n")
	if len(summary) > 2200 {
		summary = summary[:2200]
	}
	return strings.TrimSpace(summary)
}

func readTailSnippet(path string, max int) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return "", false
	}
	if max <= 0 {
		max = 600
	}
	if len(b) > max {
		b = b[len(b)-max:]
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", false
	}
	return s, true
}

func (e *Engine) executeNode(node *Node, nodeDir string) (Outcome, error) {
	if reason, blocked := e.unfixableFailureSourceReason(node); blocked {
		return Outcome{}, fmt.Errorf("%s", reason)
	}
	h := resolveHandler(node)
	maxRetries := node.IntAttr("max_retries", 0)
	allowPartial := node.BoolAttr("allow_partial", false)
	attempts := maxRetries + 1
	var out Outcome
	for attempt := 0; attempt < attempts; attempt++ {
		e.Logger.Debug("node attempt", "node", node.ID, "attempt", attempt+1, "max_attempts", attempts)
		before, err := snapshotWorkspace(e.Workspace)
		if err != nil {
			return Outcome{}, err
		}
		out, err = h.Execute(node, e.Context, e.Graph, nodeDir, e.Workspace)
		if err != nil {
			return Outcome{}, err
		}
		if out.SchemaVersion == 0 {
			out.SchemaVersion = 1
		}
		if out.ContextUpdates == nil {
			out.ContextUpdates = map[string]any{}
		}
		if node.BoolAttr("requires_tool_success", false) && out.Outcome == "success" {
			req := node.StringAttr("required_tool_node", "")
			if req != "" {
				status, err := readStatus(filepath.Join(e.RunDir, req, "status.json"))
				if err != nil || status.Outcome != "success" {
					out.Outcome = "fail"
					out.FailureReason = fmt.Sprintf("required tool node not successful: %s", req)
				}
			}
		}
		after, err := snapshotWorkspace(e.Workspace)
		if err != nil {
			return Outcome{}, err
		}
		diff := computeDiff(before, after)
		if err := writeJSON(filepath.Join(nodeDir, "workspace.diff.json"), diff); err != nil {
			return Outcome{}, err
		}
		if isExecutableNode(node) {
			allowed, err := ParseAllowedWritePaths(node)
			if err != nil {
				return Outcome{}, err
			}
			if len(allowed) > 0 {
				violations := disallowedDiffPaths(diff, allowed)
				if len(violations) > 0 {
					out.Outcome = "fail"
					out.FailureReason = fmt.Sprintf("guardrail_violation: wrote disallowed files: %s", strings.Join(violations, ","))
					_ = appendEvent(e.RunDir, map[string]any{"schema_version": 1, "type": "GuardrailViolation", "node_id": node.ID, "paths": violations, "at": time.Now().UTC().Format(time.RFC3339Nano)})
				}
			}
		}

		if out.Outcome == "retry" && attempt < attempts-1 {
			e.RetryCount[node.ID] = e.RetryCount[node.ID] + 1
			e.Context["internal.retry_count."+node.ID] = e.RetryCount[node.ID]
			_ = appendEvent(e.RunDir, map[string]any{"schema_version": 1, "type": "StageRetrying", "node_id": node.ID, "retry_count": e.RetryCount[node.ID], "at": time.Now().UTC().Format(time.RFC3339Nano)})
			e.Logger.Warn("stage requested retry", "node", node.ID, "retry_count", e.RetryCount[node.ID])
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if out.Outcome == "retry" && attempt == attempts-1 {
			if allowPartial {
				out.Outcome = "partial_success"
			} else {
				out.Outcome = "fail"
				if out.FailureReason == "" {
					out.FailureReason = "retry_exhausted"
				}
			}
		}
		return out, nil
	}
	return out, nil
}

func (e *Engine) unfixableFailureSourceReason(node *Node) (string, bool) {
	if !isCodergenNode(node) {
		return "", false
	}
	failedNodeID, _ := e.Context["last_failure.node_id"].(string)
	failedNodeID = strings.TrimSpace(failedNodeID)
	if failedNodeID == "" {
		return "", false
	}
	failedNode := e.Graph.Nodes[failedNodeID]
	if failedNode == nil || failedNode.Type() != "tool" {
		return "", false
	}
	cmd := strings.TrimSpace(failedNode.StringAttr("tool_command", ""))
	if cmd == "" {
		return "", false
	}
	sourcePaths := extractToolScriptPaths(cmd)
	if len(sourcePaths) == 0 {
		return "", false
	}
	allowed, err := ParseAllowedWritePaths(node)
	if err != nil {
		return fmt.Sprintf("invalid allowed_write_paths on node %s: %v", node.ID, err), true
	}
	if len(allowed) == 0 {
		return "", false
	}
	normalized := make([]string, 0, len(allowed))
	for _, p := range allowed {
		p = filepath.ToSlash(strings.TrimSpace(p))
		if p != "" {
			normalized = append(normalized, p)
		}
	}
	outside := []string{}
	for _, src := range sourcePaths {
		if !pathAllowed(src, normalized) {
			outside = append(outside, src)
		}
	}
	if len(outside) == 0 {
		return "", false
	}
	sort.Strings(outside)
	return fmt.Sprintf("unfixable_failure_source: failed node %s references %s outside allowed_write_paths for %s", failedNodeID, strings.Join(outside, ","), node.ID), true
}

func extractToolScriptPaths(cmd string) []string {
	tokens := strings.Fields(cmd)
	paths := []string{}
	for _, tok := range tokens {
		t := strings.Trim(tok, `"'`)
		if t == "" || strings.HasPrefix(t, "-") {
			continue
		}
		if strings.Contains(t, "=") {
			continue
		}
		if strings.HasSuffix(t, ".sh") {
			paths = append(paths, filepath.ToSlash(filepath.Clean(t)))
		}
	}
	return paths
}

func (e *Engine) writeCheckpoint(last string) error {
	completed := make([]string, 0, len(e.Completed))
	for id := range e.Completed {
		completed = append(completed, id)
	}
	sort.Strings(completed)
	cp := Checkpoint{SchemaVersion: 1, RunID: e.RunID, LastCompletedNode: last, CompletedNodes: completed, RetryCounts: e.RetryCount, Context: map[string]any(e.Context)}
	if err := writeJSON(filepath.Join(e.RunDir, "checkpoint.json"), cp); err != nil {
		return err
	}
	_ = appendEvent(e.RunDir, map[string]any{"schema_version": 1, "type": "CheckpointSaved", "last_completed_node": last, "at": time.Now().UTC().Format(time.RFC3339Nano)})
	return nil
}

func (e *Engine) selectNext(from, outcome string) string {
	var conditionals []*Edge
	var unconditionals []*Edge
	for _, edge := range e.Graph.Edges {
		if edge.From != from {
			continue
		}
		cond := strings.TrimSpace(edge.StringAttr("condition", ""))
		if cond == "" {
			unconditionals = append(unconditionals, edge)
			continue
		}
		if cond == "outcome="+outcome {
			conditionals = append(conditionals, edge)
		}
	}
	pick := conditionals
	if len(pick) == 0 {
		pick = unconditionals
	}
	if len(pick) == 0 {
		return ""
	}
	sort.Slice(pick, func(i, j int) bool {
		wi, wj := pick[i].IntAttr("weight", 0), pick[j].IntAttr("weight", 0)
		if wi != wj {
			return wi > wj
		}
		return pick[i].To < pick[j].To
	})
	return pick[0].To
}

func resolveHandler(node *Node) Handler {
	typ := node.Type()
	if typ == "" {
		switch node.Shape() {
		case "Mdiamond":
			typ = "start"
		case "Msquare":
			typ = "exit"
		case "parallelogram":
			typ = "tool"
		default:
			typ = "codergen"
		}
	}
	switch typ {
	case "start":
		return startHandler{}
	case "exit":
		return exitHandler{}
	case "tool":
		return toolHandler{}
	case "verification":
		return verificationHandler{}
	default:
		return codergenHandler{}
	}
}

type startHandler struct{}
type exitHandler struct{}

type toolHandler struct{}

type codergenHandler struct{}

func (startHandler) Execute(node *Node, _ Context, _ *Graph, _ string, _ string) (Outcome, error) {
	return Outcome{SchemaVersion: 1, Outcome: "success", SuggestedNextIDs: []string{}, ContextUpdates: map[string]any{}}, nil
}

func (exitHandler) Execute(node *Node, _ Context, _ *Graph, _ string, _ string) (Outcome, error) {
	return Outcome{SchemaVersion: 1, Outcome: "success", SuggestedNextIDs: []string{}, ContextUpdates: map[string]any{}}, nil
}

func (toolHandler) Execute(node *Node, _ Context, _ *Graph, nodeDir string, workspace string) (Outcome, error) {
	cmdText := strings.TrimSpace(node.StringAttr("tool_command", ""))
	if cmdText == "" {
		return Outcome{}, fmt.Errorf("tool_command required")
	}
	if err := validateToolCommand(cmdText); err != nil {
		return Outcome{SchemaVersion: 1, Outcome: "fail", FailureReason: err.Error(), SuggestedNextIDs: []string{}, ContextUpdates: map[string]any{}}, nil
	}
	cmd := exec.Command("sh", "-c", cmdText)
	cmd.Dir = workspace
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return Outcome{}, err
	}
	outB, _ := io.ReadAll(stdout)
	errB, _ := io.ReadAll(stderr)
	err := cmd.Wait()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			return Outcome{}, err
		}
	}
	if writeErr := os.WriteFile(filepath.Join(nodeDir, "tool.stdout.txt"), outB, 0o644); writeErr != nil {
		return Outcome{}, writeErr
	}
	if writeErr := os.WriteFile(filepath.Join(nodeDir, "tool.stderr.txt"), errB, 0o644); writeErr != nil {
		return Outcome{}, writeErr
	}
	if writeErr := os.WriteFile(filepath.Join(nodeDir, "tool.exitcode.txt"), []byte(fmt.Sprintf("%d\n", code)), 0o644); writeErr != nil {
		return Outcome{}, writeErr
	}
	outcome := "success"
	if code != 0 {
		outcome = "fail"
	}
	return Outcome{SchemaVersion: 1, Outcome: outcome, SuggestedNextIDs: []string{}, ContextUpdates: map[string]any{}, FailureReason: exitReason(code)}, nil
}

func exitReason(code int) string {
	if code == 0 {
		return ""
	}
	return fmt.Sprintf("tool_exit_code_%d", code)
}

func (codergenHandler) Execute(node *Node, ctx Context, g *Graph, nodeDir string, workspace string) (Outcome, error) {
	prompt := node.StringAttr("prompt", node.Label())
	if goal, ok := g.Attrs["goal"]; ok {
		prompt = strings.ReplaceAll(prompt, "$goal", fmt.Sprintf("%v", goal))
	}
	prompt = injectFailureFeedbackPrompt(prompt, ctx)
	prompt = injectVerificationAllowlistPrompt(prompt, node, g)
	if writeErr := os.WriteFile(filepath.Join(nodeDir, "prompt.md"), []byte(prompt+"\n"), 0o644); writeErr != nil {
		return Outcome{}, writeErr
	}
	backend := os.Getenv("ATTRACTION_BACKEND")
	if backend == "" {
		backend = os.Getenv("ATTRACTOR_BACKEND")
	}
	if backend == "fake" {
		outcome := outcomeFromTestAttrs(node, ctx)
		nextLabel := node.StringAttr("test.preferred_next_label", "")
		suggest := splitCSV(node.StringAttr("test.suggested_next_ids", ""))
		notes := node.StringAttr("test.notes", "fake backend")
		resp := fmt.Sprintf("outcome=%s\n", outcome)
		if writeErr := os.WriteFile(filepath.Join(nodeDir, "response.md"), []byte(resp), 0o644); writeErr != nil {
			return Outcome{}, writeErr
		}
		updates := map[string]any{}
		if raw := strings.TrimSpace(node.StringAttr("test.verification_plan_json", "")); raw != "" {
			var parsed any
			if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
				return Outcome{}, fmt.Errorf("invalid test.verification_plan_json: %w", err)
			}
			plan, err := ParseVerificationPlan(parsed)
			if err != nil {
				return Outcome{}, err
			}
			key := strings.TrimSpace(node.StringAttr("verification.plan_context_key", "verification.plan"))
			updates[key] = VerificationPlanToMap(plan)
		}
		return Outcome{SchemaVersion: 1, Outcome: outcome, PreferredNextLabel: nextLabel, SuggestedNextIDs: suggest, Notes: notes, ContextUpdates: updates}, nil
	}
	agent, err := ResolveAgent(node, workspace)
	if err != nil {
		return Outcome{}, err
	}
	resp, err := agent.Run(AgentRequest{
		Prompt:    prompt,
		NodeID:    node.ID,
		NodeDir:   nodeDir,
		Workspace: workspace,
		Logger:    slog.Default(),
	})
	if err != nil {
		return Outcome{}, err
	}
	if resp.ContextUpdates == nil {
		resp.ContextUpdates = map[string]any{}
	}
	if resp.VerificationPlan != nil {
		key := strings.TrimSpace(node.StringAttr("verification.plan_context_key", "verification.plan"))
		resp.ContextUpdates[key] = VerificationPlanToMap(*resp.VerificationPlan)
	}
	return Outcome{
		SchemaVersion:      1,
		Outcome:            resp.Outcome,
		PreferredNextLabel: resp.PreferredNextLabel,
		SuggestedNextIDs:   resp.SuggestedNextIDs,
		ContextUpdates:     resp.ContextUpdates,
		Notes:              resp.Notes,
		FailureReason:      resp.FailureReason,
	}, nil
}

func injectFailureFeedbackPrompt(prompt string, ctx Context) string {
	summary, _ := ctx["last_failure.summary"].(string)
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return prompt
	}
	nodeID, _ := ctx["last_failure.node_id"].(string)
	reason, _ := ctx["last_failure.reason"].(string)

	var b strings.Builder
	b.WriteString(strings.TrimRight(prompt, "\n"))
	b.WriteString("\n\nFailure feedback (from previous failed stage):\n")
	if strings.TrimSpace(nodeID) != "" {
		b.WriteString("- failed_node: ")
		b.WriteString(strings.TrimSpace(nodeID))
		b.WriteString("\n")
	}
	if strings.TrimSpace(reason) != "" {
		b.WriteString("- failure_reason: ")
		b.WriteString(strings.TrimSpace(reason))
		b.WriteString("\n")
	}
	b.WriteString("- details:\n")
	b.WriteString(summary)
	b.WriteString("\n")
	return b.String()
}

func injectVerificationAllowlistPrompt(prompt string, node *Node, g *Graph) string {
	allowed := verificationAllowedCommandsForNode(node, g)
	if len(allowed) == 0 {
		return prompt
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(prompt, "\n"))
	b.WriteString("\n\nVerification plan command allowlist (hard requirement):\n")
	for _, cmd := range allowed {
		b.WriteString("- ")
		b.WriteString(cmd)
		b.WriteString("\n")
	}
	b.WriteString("Use only these command families in verification_plan.commands.\n")
	return strings.TrimRight(b.String(), "\n")
}

func verificationAllowedCommandsForNode(node *Node, g *Graph) []string {
	if v := uniqueNonEmpty(splitCSV(node.StringAttr("verification.allowed_commands", ""))); len(v) > 0 {
		return v
	}
	seen := map[string]bool{}
	queue := []string{node.ID}
	visited := map[string]bool{}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		if cur != node.ID {
			n := g.Nodes[cur]
			if n != nil && n.Type() == "verification" {
				for _, cmd := range splitCSV(n.StringAttr("verification.allowed_commands", "")) {
					cmd = strings.TrimSpace(cmd)
					if cmd != "" {
						seen[cmd] = true
					}
				}
				continue
			}
		}
		for _, e := range g.Edges {
			if e.From == cur {
				queue = append(queue, e.To)
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for cmd := range seen {
		out = append(out, cmd)
	}
	sort.Strings(out)
	return out
}

func uniqueNonEmpty(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func outcomeFromTestAttrs(node *Node, ctx Context) string {
	seq := splitCSV(node.StringAttr("test.outcome_sequence", ""))
	if len(seq) > 0 {
		raw := ctx["internal.retry_count."+node.ID]
		idx := 0
		if i, ok := raw.(int); ok {
			idx = i
		}
		if idx < len(seq) {
			return seq[idx]
		}
		return seq[len(seq)-1]
	}
	out := node.StringAttr("test.outcome", "success")
	if out == "" {
		out = "success"
	}
	return out
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func validateToolCommand(cmd string) error {
	if strings.Contains(cmd, "~") {
		return fmt.Errorf("tool_command rejected by guardrail: contains ~")
	}
	if containsParentSegmentToken(cmd) {
		return fmt.Errorf("tool_command rejected by guardrail: contains ..")
	}
	tokens := strings.Fields(cmd)
	for _, t := range tokens {
		t = strings.Trim(t, "'\"")
		if strings.HasPrefix(t, "/") {
			return fmt.Errorf("tool_command rejected by guardrail: contains absolute path")
		}
	}
	return nil
}

func containsParentSegmentToken(cmd string) bool {
	for i := 0; i+1 < len(cmd); i++ {
		if cmd[i] != '.' || cmd[i+1] != '.' {
			continue
		}
		prevBoundary := i == 0 || isPathTokenBoundary(cmd[i-1])
		nextBoundary := i+2 >= len(cmd) || isPathTokenBoundary(cmd[i+2])
		if prevBoundary && nextBoundary {
			return true
		}
	}
	return false
}

func isPathTokenBoundary(b byte) bool {
	switch b {
	case '/', ' ', '\t', '\n', '\r', ';', '&', '|', '(', ')', '\'', '"':
		return true
	default:
		return false
	}
}

func isExecutableNode(node *Node) bool {
	t := node.Type()
	if t == "" {
		shape := node.Shape()
		return shape == "box" || shape == "parallelogram"
	}
	return t == "codergen" || t == "tool"
}

func isCodergenNode(node *Node) bool {
	t := node.Type()
	if t == "" {
		return node.Shape() == "box"
	}
	return t == "codergen"
}

func snapshotWorkspace(workspace string) (map[string]fileState, error) {
	out := map[string]fileState{}
	err := filepath.WalkDir(workspace, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(workspace, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		h := sha256.Sum256(b)
		info, err := d.Info()
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = fileState{Size: info.Size(), Hash: hex.EncodeToString(h[:])}
		return nil
	})
	return out, err
}

func computeDiff(before, after map[string]fileState) workspaceDiff {
	d := workspaceDiff{Created: []string{}, Modified: []string{}, Deleted: []string{}}
	for p, a := range after {
		b, ok := before[p]
		if !ok {
			d.Created = append(d.Created, p)
			continue
		}
		if b.Hash != a.Hash || b.Size != a.Size {
			d.Modified = append(d.Modified, p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			d.Deleted = append(d.Deleted, p)
		}
	}
	sort.Strings(d.Created)
	sort.Strings(d.Modified)
	sort.Strings(d.Deleted)
	return d
}

func disallowedDiffPaths(d workspaceDiff, allowed []string) []string {
	normalized := make([]string, 0, len(allowed))
	for _, p := range allowed {
		p = filepath.ToSlash(strings.TrimSpace(p))
		if p != "" {
			normalized = append(normalized, p)
		}
	}
	all := append([]string{}, d.Created...)
	all = append(all, d.Modified...)
	all = append(all, d.Deleted...)
	viol := []string{}
	for _, p := range all {
		if !pathAllowed(p, normalized) {
			viol = append(viol, p)
		}
	}
	sort.Strings(viol)
	return viol
}

func pathAllowed(path string, allowed []string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	for _, entry := range allowed {
		if strings.HasSuffix(entry, "/") {
			dir := strings.TrimSuffix(entry, "/")
			if path == dir || strings.HasPrefix(path, dir+"/") {
				return true
			}
			continue
		}
		if path == entry {
			return true
		}
	}
	return false
}

func findStartNode(g *Graph) *Node {
	for _, n := range g.Nodes {
		if n.Shape() == "Mdiamond" || n.ID == "start" {
			return n
		}
	}
	return nil
}

func isExit(g *Graph, id string) bool {
	n := g.Nodes[id]
	if n == nil {
		return false
	}
	return n.Shape() == "Msquare" || n.ID == "exit" || n.ID == "end"
}

func writeManifest(g *Graph, cfg RunConfig, runDir, workspace string) error {
	m := map[string]any{"schema_version": 1, "pipeline_path": cfg.PipelinePath, "original_workdir": cfg.Workdir, "workspace_path": workspace, "started_at": time.Now().UTC().Format(time.RFC3339Nano)}
	if goal, ok := g.Attrs["goal"]; ok {
		m["goal"] = goal
	}
	return writeJSON(filepath.Join(runDir, "manifest.json"), m)
}

func appendEvent(runDir string, event map[string]any) error {
	f, err := os.OpenFile(filepath.Join(runDir, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

func appendTrace(runDir, recordType string, fields map[string]any) error {
	rec := map[string]any{
		"schema_version": 1,
		"type":           recordType,
		"at":             time.Now().UTC().Format(time.RFC3339Nano),
	}
	for k, v := range fields {
		rec[k] = v
	}
	f, err := os.OpenFile(filepath.Join(runDir, "trace.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

func cloneContext(ctx Context) map[string]any {
	if ctx == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(ctx))
	for k, v := range ctx {
		out[k] = v
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func computeContextDelta(before, after map[string]any) map[string]any {
	added := map[string]any{}
	updated := map[string]any{}
	removed := []string{}
	for k, vAfter := range after {
		vBefore, ok := before[k]
		if !ok {
			added[k] = vAfter
			continue
		}
		if fmt.Sprintf("%v", vBefore) != fmt.Sprintf("%v", vAfter) {
			updated[k] = map[string]any{"before": vBefore, "after": vAfter}
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			removed = append(removed, k)
		}
	}
	sort.Strings(removed)
	return map[string]any{"added": added, "updated": updated, "removed": removed}
}

func routeCandidates(g *Graph, from, outcome string) []map[string]any {
	out := []map[string]any{}
	for _, e := range g.Edges {
		if e.From != from {
			continue
		}
		cond := strings.TrimSpace(e.StringAttr("condition", ""))
		out = append(out, map[string]any{
			"to":        e.To,
			"weight":    e.IntAttr("weight", 0),
			"condition": cond,
			"matched":   cond == "" || cond == "outcome="+outcome,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i]["to"].(string) != out[j]["to"].(string) {
			return out[i]["to"].(string) < out[j]["to"].(string)
		}
		return out[i]["condition"].(string) < out[j]["condition"].(string)
	})
	return out
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func readCheckpoint(path string) (Checkpoint, error) {
	var cp Checkpoint
	b, err := os.ReadFile(path)
	if err != nil {
		return cp, err
	}
	err = json.Unmarshal(b, &cp)
	if cp.RetryCounts == nil {
		cp.RetryCounts = map[string]int{}
	}
	if cp.Context == nil {
		cp.Context = map[string]any{}
	}
	return cp, err
}

func readStatus(path string) (Outcome, error) {
	var out Outcome
	b, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(b, &out)
	return out, err
}

func copyDir(src, dst string, excludes []string) error {
	normExcludes := make([]string, 0, len(excludes))
	for _, ex := range excludes {
		ex = strings.TrimSpace(ex)
		if ex == "" || ex == "." {
			continue
		}
		ex = filepath.ToSlash(filepath.Clean(ex))
		normExcludes = append(normExcludes, ex)
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if shouldSkipCopyRel(rel, normExcludes) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		return os.WriteFile(target, b, mode)
	})
}

func shouldSkipCopyRel(rel string, excludes []string) bool {
	for _, ex := range excludes {
		if rel == ex || strings.HasPrefix(rel, ex+"/") {
			return true
		}
	}
	return false
}

func relativeDescendant(parent, child string) (string, bool) {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	rel, err := filepath.Rel(parent, child)
	if err != nil || rel == "." {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}

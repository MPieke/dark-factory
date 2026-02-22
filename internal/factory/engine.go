package attractor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
}

func RunPipeline(cfg RunConfig) error {
	b, err := os.ReadFile(cfg.PipelinePath)
	if err != nil {
		return err
	}
	g, err := ParseDOT(string(b))
	if err != nil {
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
		return fmt.Errorf("validation failed: %s", strings.Join(msgs, "; "))
	}

	if cfg.Resume {
		if cfg.RunID == "" {
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
			return err
		}
		if err := copyDir(cfg.Workdir, workspace); err != nil {
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
		return err
	}

	e := &Engine{Graph: g, RunID: cfg.RunID, RunDir: runDir, Workspace: workspace, Context: Context{}, RetryCount: map[string]int{}, Completed: map[string]bool{}}
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
	if err := e.executeFrom(startID); err != nil {
		_ = appendEvent(runDir, map[string]any{"schema_version": 1, "type": "PipelineFailed", "error": err.Error(), "at": time.Now().UTC().Format(time.RFC3339Nano)})
		return err
	}
	_ = appendEvent(runDir, map[string]any{"schema_version": 1, "type": "PipelineCompleted", "at": time.Now().UTC().Format(time.RFC3339Nano)})
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

		e.Context["current_node"] = node.ID
		out, err := e.executeNode(node, nodeDir)
		if err != nil {
			_ = appendEvent(e.RunDir, map[string]any{"schema_version": 1, "type": "StageFailed", "node_id": node.ID, "error": err.Error(), "at": time.Now().UTC().Format(time.RFC3339Nano)})
			return err
		}
		if err := writeJSON(filepath.Join(nodeDir, "status.json"), out); err != nil {
			return err
		}
		if out.Outcome == "fail" {
			_ = appendEvent(e.RunDir, map[string]any{"schema_version": 1, "type": "StageFailed", "node_id": node.ID, "failure_reason": out.FailureReason, "at": time.Now().UTC().Format(time.RFC3339Nano)})
		} else {
			_ = appendEvent(e.RunDir, map[string]any{"schema_version": 1, "type": "StageCompleted", "node_id": node.ID, "outcome": out.Outcome, "at": time.Now().UTC().Format(time.RFC3339Nano)})
		}
		for k, v := range out.ContextUpdates {
			e.Context[k] = v
		}
		e.Context["outcome"] = out.Outcome
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
		if next == "" {
			return fmt.Errorf("no route from node %s for outcome %s", node.ID, out.Outcome)
		}
		current = next
	}
}

func (e *Engine) executeNode(node *Node, nodeDir string) (Outcome, error) {
	h := resolveHandler(node)
	maxRetries := node.IntAttr("max_retries", 0)
	allowPartial := node.BoolAttr("allow_partial", false)
	attempts := maxRetries + 1
	var out Outcome
	for attempt := 0; attempt < attempts; attempt++ {
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
	if strings.Contains(cmd, "..") {
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

func isExecutableNode(node *Node) bool {
	t := node.Type()
	if t == "" {
		shape := node.Shape()
		return shape == "box" || shape == "parallelogram"
	}
	return t == "codergen" || t == "tool"
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

func copyDir(src, dst string) error {
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
		if strings.HasPrefix(rel, ".git") {
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
		return os.WriteFile(target, b, 0o644)
	})
}

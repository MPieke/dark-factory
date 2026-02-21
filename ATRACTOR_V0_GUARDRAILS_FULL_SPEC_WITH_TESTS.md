# Attractor V0 (Guardrails Edition) — Full Spec + Full Test Spec (Single File, Codex-Ready)

This is a **V0** that still has **real guardrails** so you can “let it rip” safely:
- **Per-run isolated workspace**
- **Write-allowlist enforcement per node**
- **Tool output = truth** (AI cannot declare success without verification when configured)
- **Audit trails** (status.json, checkpoint.json, events.jsonl)
- **Backend swapability** (Codex today, custom agents tomorrow)

It also includes a **complete autonomous test spec** so Codex can validate its own work.

---

# 1) Implementation Spec (V0)

## 1.1 Goal
Implement a DOT-based pipeline runner that:
- Parses a strict DOT subset into a Graph model
- Validates graph correctness + V0 constraints
- Executes nodes deterministically (single-threaded in V0)
- Produces auditable artifacts per node
- Writes a checkpoint after each node
- Can resume from checkpoint
- Enforces guardrails (workspace isolation + allowed writes)
- Emits events as JSONL for observability

## 1.2 Non-goals (explicitly NOT in V0, but MUST be “accounted for” structurally)
Not implemented in V0 execution, but code MUST be structured so V1+ can add them without rewriting core:
- Human gates (`wait.human`)
- Parallel fan-out / fan-in (`parallel`, `parallel.fan_in`)
- Manager loop (`stack.manager_loop`)
- Model stylesheet parsing/application
- Fidelity/thread reuse
- Transform pipeline beyond `$goal` expansion
- Full condition expression language (V0 only supports minimal outcome conditions)

## 1.3 Language / Packaging
- **Go** implementation
- Single CLI binary: `attractor`

## 1.4 CLI Contract
Provide:

```bash
attractor run <pipeline.dot> --workdir <path> --runsdir <path> [--run-id <id>] [--resume]
```

Rules:
- If `--run-id` omitted, generate one suitable for filesystem paths.
- Run directory: `--runsdir/<run_id>/`
- Workspace directory: `--runsdir/<run_id>/workspace/`
- If `--resume`:
  - `--run-id` required
  - Load checkpoint from that run directory and resume

Exit codes:
- `0` success
- `1` validation error OR pipeline failure
- `2` internal error (panic recovered)

---

# 2) Execution Environment Guardrails (V0)

These are **required** and must be enforced by the engine, not “suggested” to the AI.

## 2.1 Per-run isolated workspace
On `run`, the engine MUST create:

```
<runDir>/workspace/
```

The engine MUST also create:

```
<runDir>/workspace/.attractor/
```

The engine MUST execute **all tool commands and all AI backends** with working directory set to:

```
<runDir>/workspace
```

### Workspace initialization
The engine copies (or syncs) the user-provided `--workdir` into `<runDir>/workspace` at start of a new run.
- V0 allowed approach: copy directory recursively excluding `.git` by default.
- Must be deterministic enough for tests.

On resume, do **not** recopy; resume uses the existing `<runDir>/workspace`.

## 2.2 Write allowlist enforcement (core guardrail)
Each executable node MAY define an allowlist of files it is permitted to create/modify/delete.

Node attribute:
- `allowed_write_paths` (string; comma-separated paths relative to workspace)

Rules:
- Paths are relative to workspace (no absolute paths)
- No `..` segments allowed
- Globs are NOT required in V0 (treat as literal paths only)

If `allowed_write_paths` is absent or empty:
- V0 default: **allow writes anywhere under workspace**, BUT still forbid writing outside workspace.
- (You can tighten later by making allowlist mandatory.)

### Enforcement mechanism (engine MUST do this)
Before executing a node, the engine takes a snapshot of workspace file state (V0):
- Record a manifest of all files under workspace (relative paths)
- For each file: record (size, modtime OR hash)
- Also record existence of directories is optional.

After node execution, compute the diff:
- Created files
- Modified files
- Deleted files

Then:
- If allowlist is present:
  - Every created/modified/deleted path MUST be in allowlist
  - Otherwise node outcome becomes `fail` with a clear failure_reason:
    - `guardrail_violation: wrote disallowed files: ...`
- If allowlist is empty:
  - Only enforce “must be within workspace” (see 2.3)

Artifacts:
- Write `<node_id>/workspace.diff.json` containing lists of created/modified/deleted files.

## 2.3 “No escape” rule (must not write outside workspace)
The engine MUST ensure that neither tool commands nor AI backends can write outside workspace.

V0 enforcement (practical, testable):
- Run all commands with `Cmd.Dir = workspace`
- Reject any tool_command that contains:
  - an absolute path prefix (`/` on unix) OR
  - `..` path segments OR
  - `~` home expansion
  - (This is a heuristic guardrail, acceptable for V0.)

Additionally:
- The workspace diff enforcement in 2.2 ensures only workspace changes are considered. But you still must block obvious attempts in tool_command.

## 2.4 “Tool output = truth” (optional but supported in V0)
V0 must support a *simple truth gate* without implementing full scenarios.

Node attributes:
- `requires_tool_success` (bool, default false)
- `required_tool_node` (string; node id of a tool node that must succeed before pipeline can exit or before this node can be considered success)

V0 interpretation:
- If `requires_tool_success=true` on a codergen node:
  - That codergen node’s outcome cannot be final “success” unless the referenced tool node has succeeded at least once in the run.
- This is a minimal hook; deeper correctness comes from your pipeline structure (implement → test → evaluate).

---

# 3) DOT Parsing (V0 Subset)

## 3.1 Supported
- One `digraph` per file
- `graph [ ... ]` attrs
- `node [ ... ]` defaults
- `edge [ ... ]` defaults
- Node stmt: `ID [k=v, ...]`
- Edge stmt: `A -> B [k=v, ...]`
- Chained edges: `A -> B -> C [attrs]` expands accordingly

## 3.2 Unsupported (ERROR)
- Undirected edges (`--`)
- Multiple graphs
- Subgraphs/clusters
- HTML labels

## 3.3 Identifiers and Values
Node IDs: `[A-Za-z_][A-Za-z0-9_]*`

Value types:
- String `"..."` (with escapes)
- Integer
- Float (parse + preserve)
- Boolean `true|false`
- Duration `<int>(ms|s|m|h|d)` → time.Duration

## 3.4 Defaults merging
- `node [..]` applies to subsequent nodes
- `edge [..]` applies to subsequent edges

## 3.5 Preserve unknown keys
Parser MUST preserve all attrs in maps for graph/nodes/edges.

---

# 4) Core Model Types (V0)

## 4.1 Graph
- `Nodes map[string]*Node`
- `Edges []*Edge`
- `Attrs map[string]Value`

## 4.2 Node
- `ID string`
- `Attrs map[string]Value`

Derived:
- `shape` default `"box"`
- `type` default `""`
- `label` default `ID`
- `prompt` default `""`
- `max_retries` default `0`
- `timeout` optional
- `allowed_write_paths` parsed into []string (optional)
- `requires_tool_success` bool (optional)
- `required_tool_node` string (optional)

## 4.3 Edge
- `From, To string`
- `Attrs map[string]Value`

Derived:
- `condition` string default `""`
- `weight` int default `0`
- `label` string default `""`

---

# 5) Validation (V0)

Diagnostics: `ERROR|WARNING|INFO`. Any ERROR => no execution.

## 5.1 Required errors
- Exactly one start node:
  - `shape=="Mdiamond"` OR `id=="start"`
- At least one exit node:
  - `shape=="Msquare"` OR id in {"exit","end"}
- Start has no incoming edges
- Exit nodes have no outgoing edges
- Edge targets exist
- All nodes reachable from start
- Any subgraph present => ERROR
- Condition syntax valid under V0 condition subset
- Unsupported handler shapes/types present => ERROR (see 5.2)
- `allowed_write_paths` entries must be valid relative paths (no absolute, no `..`, no empty)

## 5.2 Unsupported handlers in V0 (ERROR)
Reject nodes that would require these handlers:
- `hexagon` / `type="wait.human"`
- `diamond` / `type="conditional"`
- `component` / `type="parallel"`
- `tripleoctagon` / `type="parallel.fan_in"`
- `house` / `type="stack.manager_loop"`

V0 supported:
- `Mdiamond` => start
- `Msquare` => exit
- `box` => codergen
- `parallelogram` => tool
(and explicit `type` overrides for these)

---

# 6) Execution Engine (V0)

## 6.1 Traversal
- Start at start node
- Execute node handler with retries
- Merge context updates
- Write checkpoint
- Select next edge deterministically
- Stop when reaching exit node

## 6.2 Context
JSON-serializable map `map[string]any`.

Engine keys:
- `graph.goal` (if present)
- `current_node`
- `outcome`
- `internal.retry_count.<node_id>`

## 6.3 Outcome + status.json (stable contract)
Write to `<runDir>/<node_id>/status.json`

Schema:
```json
{
  "schema_version": 1,
  "outcome": "success | fail | retry | partial_success",
  "preferred_next_label": "",
  "suggested_next_ids": [],
  "context_updates": {},
  "notes": "",
  "failure_reason": ""
}
```

## 6.4 Retry
- total attempts = `max_retries + 1`
- retry if outcome == `retry`
- fixed 500ms sleep between retries
- if exhausted:
  - if `allow_partial=true` => `partial_success`
  - else `fail`

## 6.5 Checkpoint
Write `<runDir>/checkpoint.json` after each node.

Schema:
```json
{
  "schema_version": 1,
  "run_id": "<id>",
  "last_completed_node": "<node_id>",
  "completed_nodes": ["..."],
  "retry_counts": {"node": 1},
  "context": {}
}
```

Resume:
- load checkpoint
- restore context + retry_counts + completed_nodes
- continue from next node

## 6.6 Run directory structure
`<runsdir>/<run_id>/`
- `manifest.json`
- `events.jsonl`
- `checkpoint.json`
- `workspace/`
- `<node_id>/`
  - `status.json`
  - `prompt.md` (codergen)
  - `response.md` (codergen)
  - `tool.stdout.txt` (tool)
  - `tool.stderr.txt` (tool)
  - `workspace.diff.json` (guardrail)
  - `tool.exitcode.txt` (tool; optional but helpful)

`manifest.json` includes:
- schema_version=1
- pipeline path
- original workdir
- workspace path
- started_at
- goal (if present)

## 6.7 Events JSONL
Write JSON lines `{schema_version:1, type:"...", ...}`.

Required events:
- PipelineStarted
- StageStarted
- StageCompleted
- StageFailed
- StageRetrying
- GuardrailViolation (new; required when allowlist violated)
- CheckpointSaved
- PipelineCompleted
- PipelineFailed

---

# 7) Handlers (V0)

## 7.1 Handler interface
```go
type Handler interface {
  Execute(node *Node, ctx Context, g *Graph, runDir string, workspace string) (Outcome, error)
}
```

## 7.2 Registry
Resolve in order:
1) node.type
2) shape mapping
3) default = codergen

## 7.3 Start / Exit
No-op, success.

## 7.4 Tool handler
- requires `tool_command`
- reject tool_command if it contains absolute paths, `..`, or `~` (V0 heuristic)
- execute with working dir = workspace
- capture stdout/stderr
- outcome success if exit code 0 else fail

## 7.5 Codergen handler
Two modes:

### Fake backend (required for tests)
Controlled by env var or CLI flag (pick one and document):
- `ATTRACTION_BACKEND=fake`

Behavior:
- Write `<node_id>/prompt.md` and `<node_id>/response.md`
- Determine outcome from node attrs:
  - `test.outcome` (success|fail|retry|partial_success)
  - `test.preferred_next_label`
  - `test.suggested_next_ids` (comma-separated)
- Optional: write notes

### Real backend (optional in V0)
- Call Codex CLI or any backend process
- Must run with working dir = workspace
- Must still log prompt/response
- Outcome can default to success if you don’t parse structured output in V0

Prompt selection:
- node.prompt if set else node.label
- expand `$goal` from graph attr `goal`

---

# 8) Conditions + Edge Selection (V0)

## 8.1 Condition syntax (V0 only)
Allow only:
- `outcome=success`
- `outcome=fail`
- `outcome=retry`
- `outcome=partial_success` (recommended so partial_success can route like success)

Empty condition = unconditional.
Anything else = validation ERROR.

## 8.2 Edge selection algorithm
1) Prefer edges whose condition matches the current outcome (if any)
2) Else consider unconditional edges
3) Pick highest weight
4) Tie-break by `to` lexicographically

If no outgoing edges:
- if current node is exit => success
- else => pipeline fail (no route)

---

# 9) Definition of Done (V0)
DONE when:
- go test ./... passes
- scripts/smoke.sh passes
- guardrails are enforced and tested:
  - workspace created
  - tool_command heuristics block obvious escapes
  - allowlist violations are detected and fail the node
  - workspace.diff.json emitted

---

# 2) Test Spec (V0) — Autonomous Verification

All tests run headlessly with fake backend.

## 2.1 Test harness requirements
- `go test ./...`
- `./scripts/smoke.sh`
- Tests create temp dirs:
  - workdir (source)
  - runsdir (output)
  - pipelines (dot files)

## 2.2 Parser tests

### PARSE_001: Minimal pipeline parses
linear: start->a->exit  
Assert node/edge counts.

### PARSE_002: Chained edges expand
`start -> a -> b -> exit [weight=2]`  
Assert 3 edges, weight=2.

### PARSE_003: Defaults apply
node defaults apply to later nodes; edge defaults to later edges.

### PARSE_004: Unknown attrs preserved
Ensure unknown keys remain in attrs maps.

## 2.3 Validation tests

### VALID_001: exactly one start
missing or multiple => error.

### VALID_002: at least one exit
missing => error.

### VALID_003: missing target
edge to missing node => error.

### VALID_004: reachability
orphan => error.

### VALID_005: unsupported shapes rejected
hexagon => error.

### VALID_006: condition restricted
`context.foo=true` => error; `outcome=success` ok.

### VALID_007: invalid allowlist paths rejected
- `allowed_write_paths="/etc/passwd"` => error
- `allowed_write_paths="../x"` => error
- `allowed_write_paths=""` allowed (means no allowlist)

## 2.4 Execution tests (fake backend)

### EXEC_001: linear run produces artifacts
Pipeline start->a->exit.
Assert:
- run dir created
- a/status.json exists
- events.jsonl includes PipelineStarted and PipelineCompleted
- checkpoint.json exists

### EXEC_002: tool node captures output
tool_command prints stdout/stderr.
Assert files contain expected output.

### EXEC_003: routing by outcome
Node a sets `test.outcome=fail`
Edge conditions route to exit_fail.
Assert exit_fail reached.

### EXEC_004: weight tie-break
Higher weight edge chosen; equal weight uses lexicographic tie-break.

## 2.5 Retry tests

### RETRY_001: retries honored
a max_retries=2, fake backend returns retry twice then success.
Assert 3 executions and StageRetrying events.

### RETRY_002: retry exhaustion fails
Always retry; after max attempts node fails and pipeline fails.

### RETRY_003: allow_partial converts exhaustion
allow_partial=true; exhaustion yields partial_success and pipeline can complete (treat partial_success like success).

## 2.6 Guardrail tests (MOST IMPORTANT)

### GUARD_001: workspace created and used
Create source workdir with a file `seed.txt`.
Run pipeline with a tool node: `cat seed.txt`.
Assert tool stdout contains content.
Assert that file exists inside `<runDir>/workspace/seed.txt` (copied).

### GUARD_002: allowlist permits only specific writes
Setup: workspace initially contains `a.txt` and `b.txt`.
Pipeline:
- codergen node (fake backend does NOT actually write; so use tool to simulate writing) OR implement a test-only helper tool node.
Better: tool node runs `sh -c 'echo hi > a.txt'`.
Set `allowed_write_paths="a.txt"` on that tool node (guardrails apply to ALL executable nodes).
Run.
Assert success, and diff.json shows modified `a.txt` only.

### GUARD_003: allowlist violation fails
Same but tool writes `b.txt` while allowlist is `a.txt`.
Assert node fails with failure_reason containing `guardrail_violation`.
Assert GuardrailViolation event emitted.
Assert workspace.diff.json lists b.txt as modified/created.

### GUARD_004: tool_command escape heuristics block obvious attempts
Pipeline tool node with command containing `../`:
`tool_command="sh -c 'echo x > ../oops.txt'"`
Assert validation OR execution fails with clear message (choose one; execution-time rejection is fine).
Assert file `oops.txt` is NOT created outside workspace.

### GUARD_005: “no writes outside workspace” sanity check
In test harness, create a parent temp dir and a sentinel file outside workspace.
Attempt tool command that tries to overwrite it with absolute path (e.g. `/tmp/...`).
Engine must reject tool_command containing absolute paths in V0.
Assert sentinel unchanged.

## 2.7 Resume tests

### RESUME_001: resume does not rerun completed node
Provide a test-only stop hook:
- env var `ATTRACTION_TEST_STOP_AFTER_NODE=<id>` that causes engine to exit after checkpointing that node with PipelineFailed reason `test_stop`.

Pipeline start->a->b->exit.
First run stops after a.
Resume run continues at b only.
Assert:
- second run events start with StageStarted(b)
- a not rerun (no new attempts added)

## 2.8 Smoke script
`scripts/smoke.sh` must:
1) build binary
2) run a tiny pipeline in fake backend mode
3) verify status.json exists

---

# 3) One Codex Prompt (copy/paste)

Implement `attractor` in Go that satisfies this spec:
- Core runner (parse/validate/execute)
- Checkpoint + resume
- Events JSONL
- Handlers: start, exit, codergen (fake backend required), tool
- **Guardrails**:
  - per-run workspace
  - allowlist enforcement + diff.json
  - reject tool_command with absolute paths, `..`, or `~`
- Implement full test suite and smoke script
- Run `go test ./...` until green

Do NOT implement V1 features. Reject them via validation errors only.

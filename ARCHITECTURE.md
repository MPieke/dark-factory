# Architecture

## Purpose
`dark-factory` is a Go CLI (`attractor`) that executes a v0 DAG-like pipeline defined in DOT (`digraph`) and records deterministic run artifacts.

## High-level flow
1. CLI parses `run` command args and builds `RunConfig`.
2. Engine reads/parses DOT into an in-memory graph.
3. Validator enforces structural and v0 compatibility constraints.
4. Engine prepares run directory and workspace snapshot copy.
5. Engine executes nodes from `start` to an `exit` node.
6. Engine persists per-stage status, workspace diffs, checkpoints, and event log.

## Components
- `cmd/attractor/main.go`
  - CLI entrypoint and argument validation.
- `internal/attractor/parser.go`
  - DOT parsing, attribute parsing, and primitive value coercion.
- `internal/attractor/model.go`
  - Graph/Node/Edge models and attribute helpers.
- `internal/attractor/validate.go`
  - Semantic validation (start/exit constraints, supported node/edge types, reachability).
- `internal/attractor/engine.go`
  - Runtime orchestration, handler dispatch, retries, guardrails, checkpoint/resume, artifacts.
- `internal/attractor/agent.go`
  - Agent interface and backend resolution.
- `internal/attractor/agent_codex.go`
  - Codex CLI adapter implementation.

## Data model
- Graph:
  - `Nodes map[string]*Node`
  - `Edges []*Edge`
  - `Attrs map[string]any` (graph-level attrs like `goal`)
- Node:
  - `ID`
  - `Attrs` (shape, type, prompt, tool_command, retry controls, guardrail settings, test attrs)
- Edge:
  - `From`, `To`, `Attrs` (e.g., `condition`, `weight`)

## Execution model
- Start node:
  - Shape `Mdiamond` or id `start`.
- Exit nodes:
  - Shape `Msquare` or id `exit`/`end`.
- Handler resolution:
  - `start` handler
  - `exit` handler
  - `tool` handler (`parallelogram` / `type=tool`)
  - `codergen` handler (default for executable box nodes)

Stage loop behavior:
- Execute node handler.
- Persist `status.json`.
- Merge `context_updates` into run context.
- Write checkpoint.
- Select next edge based on conditional match (`condition="outcome=..."`), else unconditional; tie-break by highest `weight`.

## Artifacts
Per-run directory (`<runsdir>/<run-id>/`):
- `manifest.json`
- `events.jsonl`
- `checkpoint.json`
- `workspace/` (copied source workdir)
- Per-node dir:
  - `status.json`
  - `workspace.diff.json`
  - `prompt.md` and `response.md` (codergen)
  - `tool.stdout.txt`, `tool.stderr.txt`, `tool.exitcode.txt` (tool)

## Resume model
- `--resume --run-id <id>` reloads checkpoint and completed node state.
- Engine computes next node from last completed node outcome.
- If last completed is an exit node, resume is effectively complete.

## Backend behavior (v0)
- Codergen prompt is assembled and written to `prompt.md`.
- Fake mode remains available via `ATTRACTION_BACKEND=fake` (or `ATTRACTOR_BACKEND=fake`) for deterministic tests.
- Real execution uses an `Agent` interface (`ResolveAgent`), making backend swap straightforward.
- Built-in backends:
  - `stub` (default)
  - `codex` (CLI-driven)
- Codex backend configuration supports sandbox mode, approval policy, working dir, additional dirs, and raw `-c key=value` overrides.

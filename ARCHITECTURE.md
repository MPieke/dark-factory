# Architecture

## Purpose
`dark-factory` is a Go CLI (`factory`) that executes a v0 DAG-like pipeline defined in DOT (`digraph`) and records deterministic run artifacts.

## High-level flow
1. CLI parses `run` command args and builds `RunConfig`.
2. Engine reads/parses DOT into an in-memory graph.
3. Validator enforces structural and v0 compatibility constraints.
4. Engine prepares run directory and workspace snapshot copy.
5. Engine executes nodes from `start` to an `exit` node.
6. Engine persists per-stage status, workspace diffs, checkpoints, and event log.
7. Engine appends structured session trace records for inputs/outputs/transforms/routing.

## Components
- `cmd/factory/main.go`
  - CLI entrypoint and argument validation.
- `internal/factory/parser.go`
  - DOT parsing, attribute parsing, and primitive value coercion.
- `internal/factory/model.go`
  - Graph/Node/Edge models and attribute helpers.
- `internal/factory/validate.go`
  - Semantic validation (start/exit constraints, supported node/edge types, reachability).
- `internal/factory/engine.go`
  - Runtime orchestration, handler dispatch, retries, guardrails, checkpoint/resume, artifacts.
- `internal/factory/agent.go`
  - Agent interface and backend resolution.
- `internal/factory/agent_codex.go`
  - Codex CLI adapter implementation.
- `internal/factory/verification.go`
  - Deterministic verification handler that executes structured verification plans from context.
- `internal/factory/verification_plan.go`
  - Verification plan schema/parsing and safe relative-path normalization.

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
  - `verification` handler (`type=verification`)
  - `codergen` handler (default for executable box nodes)

Stage loop behavior:
- Execute node handler.
- Persist `status.json`.
- Merge `context_updates` into run context.
- Write checkpoint.
- Select next edge based on conditional match (`condition="outcome=..."`), else unconditional; tie-break by highest `weight`.

Verification stage behavior (`type=verification`):
- Reads a structured verification plan from context (default key: `verification.plan`).
- Plan includes required files and commands.
- Enforces per-node command prefix allowlist (`verification.allowed_commands`).
- Writes `verification.plan.json` and `verification.results.json`.

## Artifacts
Per-run directory (`<runsdir>/<run-id>/`):
- `manifest.json`
- `events.jsonl`
- `trace.jsonl`
- `checkpoint.json`
- `workspace/` (copied source workdir)
- Per-node dir:
  - `status.json`
  - `workspace.diff.json`
  - `prompt.md` and `response.md` (codergen)
  - `tool.stdout.txt`, `tool.stderr.txt`, `tool.exitcode.txt` (tool)
  - `verification.plan.json`, `verification.results.json` (verification)

`trace.jsonl` includes records such as:
- `SessionInitialized`
- `PipelineStarted` / `PipelineCompleted` / `PipelineFailed`
- `NodeInputCaptured`
- `NodeOutputCaptured` (including context delta)
- `RouteEvaluated`

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
- Codex responses can optionally include a structured `verification_plan` object; engine stores it in context for verification nodes.

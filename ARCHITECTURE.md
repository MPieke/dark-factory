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
- `internal/factory/logging.go`
  - Structured runtime logger (`slog`) with env-configurable level/format.
- `internal/factory/agent.go`
  - Agent interface and backend resolution.
- `internal/factory/agent_codex.go`
  - Codex CLI adapter implementation, timeout/heartbeat behavior, and live stream capture.
- `internal/factory/verification.go`
  - Deterministic verification handler that executes structured verification plans from context.
- `internal/factory/verification_plan.go`
  - Verification plan schema/parsing and safe relative-path normalization.
- `scripts/scenarios/preflight_scenario.sh`
  - Shared scenario harness that runs deterministic `selftest` checks and optional `live` checks.
  - Classifies live failures as `infra` or `product` and emits `failure_class=...` for diagnostics.
- `scripts/scenarios/lint_scenarios.sh`
  - Scenario guardrail linter for contract and portability violations (hardcoded live model defaults, fixed `/tmp` files).

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
- For codergen stages, runtime can stop early with an `unfixable_failure_source` error when the previous failed tool stage references script paths outside current `allowed_write_paths`.

Verification stage behavior (`type=verification`):
- Reads a structured verification plan from context (default key: `verification.plan`).
- Plan includes required files and commands.
- Enforces per-node command prefix allowlist (`verification.allowed_commands`).
- Executes commands from workspace root by default, or from `verification.workdir` when configured.
- Writes `verification.plan.json` and `verification.results.json`.

Scenario validation contract:
- Scenario scripts support `SCENARIO_MODE=selftest|live`.
- `selftest` mode must be deterministic and succeed unless the scenario logic is broken.
- `live` mode validates real integrations (provider/API/network) when enabled.
- Shared runner `scripts/scenarios/preflight_scenario.sh` enforces this sequence.
- On stage failure, engine stores structured feedback in context (`last_failure.*`) from stage artifacts (reason, stderr/stdout tails, and artifact paths).
- Codergen nodes automatically append a `Failure feedback` section to the prompt when `last_failure.summary` exists.

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
  - `codex.args.txt`, `codex.stdout.log`, `codex.stderr.log` (codex backend)
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
- Codex backend executable path is configurable (`codex.path` / `ATTRACTOR_CODEX_PATH`), including workspace-relative wrapper paths.
- Codex MCP can be disabled per-node/env (`codex.disable_mcp` / `ATTRACTOR_CODEX_DISABLE_MCP`), which injects `-c mcp_servers.memory_ops.enabled=false`.
- Codex backend read isolation:
  - by default, `scripts/scenarios/` is hidden from Codex nodes during execution.
  - this prevents builder agents from reading holdout scenario validators.
  - optional strict scope mode (`codex.strict_read_scope`) hides all workspace entries except `codex.workdir` + `codex.add_dirs` during execution.
  - configurable via:
    - `codex.block_read_paths` / `ATTRACTOR_CODEX_BLOCK_READ_PATHS`
    - `codex.allow_read_scenarios=true` (opt-out of default scenario hide)
- Codex backend supports execution controls:
  - `codex.timeout_seconds` / `ATTRACTOR_CODEX_TIMEOUT_SECONDS`
  - `codex.heartbeat_seconds` / `ATTRACTOR_CODEX_HEARTBEAT_SECONDS`
- Codex stream visibility:
  - `FACTORY_LOG_CODEX_STREAM=1` enables live stdout/stderr line logging to the factory logger.
  - stdout/stderr are also written incrementally to per-node files while the process is running.
- Codex responses can optionally include a structured `verification_plan` object; engine stores it in context for verification nodes.

## Workspace copy rules
- Run workspace is copied from `--workdir` into `<runsdir>/<run-id>/workspace`.
- Engine excludes `.git` during copy.
- File modes are preserved during workspace copy (including executable bits).
- If `--runsdir` is nested under `--workdir` (for example `workdir/.runs`), the nested runs path is automatically excluded from copy to prevent recursive self-copy loops.

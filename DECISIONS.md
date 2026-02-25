# Design Decisions

This document captures key logic choices in v0, especially around safety, permissions, and deterministic execution.

## 1) Strict pipeline subset (v0 only)
Decision:
- Support a constrained DOT subset and explicitly reject v1+ patterns.

Why:
- Keeps parser/validator simple and predictable.
- Reduces accidental ambiguity in runtime behavior.

Examples:
- Reject unsupported handlers/shapes (`conditional`, `parallel`, etc.).
- Require exactly one start and at least one exit.
- Require reachability from start.

## 2) Deterministic routing
Decision:
- Route by `outcome` conditions first; if no match, use unconditional edges.
- If multiple candidates match, highest `weight` wins; stable tie-break by target ID.

Why:
- Makes execution explainable and reproducible.
- Prevents hidden non-determinism from map iteration order.

## 3) Workspace isolation by copy
Decision:
- Each run gets its own `workspace/` copied from `--workdir`.
- If `--runsdir` is inside `--workdir`, exclude that nested path from copy.

Why:
- Avoids mutating source state directly.
- Produces auditable per-run effects.
- Simplifies resume/checkpoint behavior.

Tradeoff:
- Copying can be slower for large repos.
- Nested runsdir exclusion avoids recursive copy explosions but requires path normalization logic in copy step.

## 4) File write guardrails (`allowed_write_paths`)
Decision:
- Node-level allowlist can restrict changed files to exact relative paths and directory prefixes.
- Directory entries are expressed with trailing slash (example: `src/`).
- Guardrails fail the stage if diffs include paths outside the allowlist.
- Validation rejects absolute paths, parent segments (`..`), and empty entries.

Why:
- Limits blast radius of executable nodes.
- Keeps write permissions explicit in pipeline definitions.

Tradeoff:
- Directory allowlists increase flexibility but broaden write scope; critical files should still use exact path entries.

## 5) Structured verification plans
Decision:
- Add a dedicated `verification` node type that consumes a structured plan from context.
- Plan contains required file paths and commands to execute.
- Verification commands are restricted by `verification.allowed_commands` command-prefix allowlist.
- Persist verification inputs/outputs as artifacts (`verification.plan.json`, `verification.results.json`).

Why:
- Keeps verification auditable and deterministic while still allowing LLM-authored plans.
- Separates implementation freedom from deterministic, policy-checked validation execution.
- Makes behavior-based checks practical even when file layout is flexible.

## 6) Tool command guardrails
Decision:
- Reject `tool_command` containing `~`, `..`, or absolute path tokens.

Why:
- Blocks common path-escape patterns.
- Encourages workspace-relative command execution.

Tradeoff:
- This is not a full shell sandbox; it is a practical baseline filter.

## 7) Retry semantics
Decision:
- `max_retries` yields `attempts = max_retries + 1`.
- If retries are exhausted:
  - `allow_partial=true` -> `partial_success`
  - else -> `fail` with `retry_exhausted` fallback reason.

Why:
- Captures “best effort” versus “must succeed” behavior explicitly.
- Keeps retry policy local to node config.

## 8) Event + checkpoint persistence
Decision:
- Persist both append-only events and stateful checkpoints.
- Persist a structured session trace stream for execution transparency (`trace.jsonl`).

Why:
- `events.jsonl` provides timeline/audit.
- `checkpoint.json` provides minimal resume state.
- `trace.jsonl` captures node inputs/outputs/context transformations/route decisions for replay-grade debugging.
- Combined model improves operational clarity.

## 9) Prompt handling and AI backend in v0
Decision:
- Build and persist `prompt.md`.
- Keep a pluggable `Agent` interface so codergen execution backend can be swapped.
- Provide built-in `codex` adapter plus `stub` and `fake` modes.

Why:
- Decouples pipeline runtime logic from any one agent provider.
- Allows real runs through Codex CLI while preserving deterministic test mode.

## 10) Agent permission/sandbox controls
Decision:
- Expose Codex controls as config, not hardcoded policy:
  - sandbox mode
  - approval policy
  - working directory
  - additional writable directories
  - raw config overrides (`-c key=value`)
  - optional auto-approve command list mapped through a configurable Codex config key

Why:
- Keeps security posture explicit per environment/pipeline.
- Avoids baking provider-specific policy assumptions into engine code.
- Leaves room for different agent providers with different control surfaces.

## 11) Runtime observability as first-class behavior
Decision:
- Use structured logs for pipeline and stage lifecycle.
- Add Codex execution lifecycle logs (start, heartbeat, completion/failure/timeout).
- Persist Codex invocation details (`codex.args.txt`) and stream output logs.
- Support opt-in live stream logging with `FACTORY_LOG_CODEX_STREAM=1`.

Why:
- Removes blind spots where long-running agent nodes appear idle.
- Makes hangs/timeouts diagnosable without manual postmortem guesswork.
- Keeps default noise reasonable while allowing high-visibility debugging when needed.

## 12) Exit codes and error classes
Decision:
- CLI returns:
  - `1` for normal failures/usage errors.
  - `2` for panic/internal error path.

Why:
- Keeps command-line behavior simple while distinguishing severe internal failures.

## 13) Scenario validation uses split preflight modes
Decision:
- Standardize scenario scripts on:
  - `SCENARIO_MODE=selftest` for deterministic script correctness checks.
  - `SCENARIO_MODE=live` for real external/API behavior checks.
- Use shared runner `scripts/scenarios/preflight_scenario.sh` to run selftest first, then live checks when `REQUIRE_LIVE=1`.

Why:
- Avoids conflating scenario bugs with provider/network/config failures.
- Fails fast on broken scenario logic before expensive fix loops.
- Keeps validation policy reusable across scenario types.

## 14) Testing philosophy
Decision:
- Use spec-first and test-first development as the default.
- Prefer autonomous, executable validation (AI-run tests) over manual inspection.
- Escalate to human judgment only when specs or results are unclear.

Why:
- Reduces reward-hacking pressure and ambiguous interpretation.
- Increases reproducibility and confidence in behavior changes.
- Keeps delivery loop fast while maintaining safety constraints.

## 15) Holdout scenario isolation for Codex nodes
Decision:
- Apply a runtime read-isolation safeguard in the Codex backend:
  - default blocked read path: `scripts/scenarios/`
  - blocked paths are physically hidden from workspace before Codex execution and restored after execution
- Keep node-level scoping (`codex.workdir`, `codex.add_dirs`) for least-privilege context.
- Allow explicit opt-out only when intentional:
  - `codex.allow_read_scenarios=true`
  - optional custom blocked list via `codex.block_read_paths` / `ATTRACTOR_CODEX_BLOCK_READ_PATHS`

Why:
- Keeps user scenario scripts as external validation criteria instead of in-model hints.
- Reduces reward hacking by preventing direct scenario overfitting.
- Enforces isolation at runtime rather than relying only on prompt policy.

Tradeoff:
- Agent nodes have less context and may need more fix iterations for scenario-derived failures.

## 16) Go cache writes must stay inside allowed write scope
Decision:
- Agent prompts now require `GOCACHE="$PWD/.gocache"` while running from `agent/`.
- Keep `allowed_write_paths="agent/"` rather than broadening to workspace-level cache paths.

Why:
- Prevents guardrail false failures caused by cache writes under workspace root (`.gocache/...`).
- Preserves tight write boundaries without weakening policy.

## 17) Verification allowlist matches normalized command intent
Decision:
- Verification allowlist matching now normalizes commands before prefix checks:
  - strips leading env assignments (for example `GOCACHE=...`)
  - strips leading `cd ... &&` and `export ... &&` wrappers
  - trims wrapping parentheses

Why:
- Keeps allowlist policy focused on effective command intent (`go test`, `go build`) instead of fragile shell wrappers.
- Reduces false-negative verification failures while preserving command-prefix guardrails.

## 18) Builder prompts must avoid orchestrator metadata
Decision:
- Agent builder nodes should consume a product-only builder spec, not orchestration context.
- Pipeline metadata (layers, routing, node strategy, validation topology) stays outside builder-visible spec files.

Why:
- Reduces implementation bias toward pipeline mechanics.
- Keeps agent behavior aligned with product outcomes rather than orchestrator internals.
- Preserves clean separation between build intent and evaluation policy.

## 19) Strict Codex read scope for builder nodes
Decision:
- Support `codex.strict_read_scope=true` to restrict Codex-readable workspace paths to:
  - `codex.workdir`
  - explicit `codex.add_dirs`
- At runtime, all other top-level workspace entries are temporarily hidden during Codex execution.

Why:
- Prevents accidental reads of repo memory/docs/orchestrator metadata via `..` traversal.
- Makes builder context explicitly least-privilege instead of convention-based.

## 20) Verification execution directory must be configurable
Decision:
- Added `verification.workdir` (relative to workspace) for verification nodes.
- Verification commands default to workspace root when unset.

Why:
- Generated verification commands are often relative to app directory context.
- Running all verification commands from workspace root causes avoidable path/cwd failures.

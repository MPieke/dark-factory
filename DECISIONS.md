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

Why:
- Avoids mutating source state directly.
- Produces auditable per-run effects.
- Simplifies resume/checkpoint behavior.

Tradeoff:
- Copying can be slower for large repos.

## 4) File write guardrails (`allowed_write_paths`)
Decision:
- Node-level allowlist can restrict changed files to exact relative paths.
- Guardrails fail the stage if diffs include paths outside the allowlist.
- Validation rejects absolute paths, parent segments (`..`), and empty entries.

Why:
- Limits blast radius of executable nodes.
- Keeps write permissions explicit in pipeline definitions.

Tradeoff:
- Current matching is exact-path only (no globs/directories).

## 5) Tool command guardrails
Decision:
- Reject `tool_command` containing `~`, `..`, or absolute path tokens.

Why:
- Blocks common path-escape patterns.
- Encourages workspace-relative command execution.

Tradeoff:
- This is not a full shell sandbox; it is a practical baseline filter.

## 6) Retry semantics
Decision:
- `max_retries` yields `attempts = max_retries + 1`.
- If retries are exhausted:
  - `allow_partial=true` -> `partial_success`
  - else -> `fail` with `retry_exhausted` fallback reason.

Why:
- Captures “best effort” versus “must succeed” behavior explicitly.
- Keeps retry policy local to node config.

## 7) Event + checkpoint persistence
Decision:
- Persist both append-only events and stateful checkpoints.

Why:
- `events.jsonl` provides timeline/audit.
- `checkpoint.json` provides minimal resume state.
- Combined model improves operational clarity.

## 8) Prompt handling and AI backend in v0
Decision:
- Build and persist `prompt.md`.
- Keep a pluggable `Agent` interface so codergen execution backend can be swapped.
- Provide built-in `codex` adapter plus `stub` and `fake` modes.

Why:
- Decouples pipeline runtime logic from any one agent provider.
- Allows real runs through Codex CLI while preserving deterministic test mode.

## 9) Agent permission/sandbox controls
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

## 10) Exit codes and error classes
Decision:
- CLI returns:
  - `1` for normal failures/usage errors.
  - `2` for panic/internal error path.

Why:
- Keeps command-line behavior simple while distinguishing severe internal failures.

## 11) Testing philosophy
Decision:
- Use spec-first and test-first development as the default.
- Prefer autonomous, executable validation (AI-run tests) over manual inspection.
- Escalate to human judgment only when specs or results are unclear.

Why:
- Reduces reward-hacking pressure and ambiguous interpretation.
- Increases reproducibility and confidence in behavior changes.
- Keeps delivery loop fast while maintaining safety constraints.

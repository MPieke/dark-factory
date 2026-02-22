# Pipeline Guidelines

This guide explains how to create pipeline DOT files for `dark-factory` v0.

## Mental model
- A pipeline is a directed graph (`digraph`) of stages.
- Each stage returns an `outcome` (`success`, `fail`, `retry`, `partial_success`).
- Edges can route based on outcome (`condition="outcome=..."`).
- Execution starts at one `start` node and ends at an `exit` node.

Think of it as a small state machine:
- node runs
- outcome is produced
- next edge is chosen
- repeat until exit

## Required structure (v0)
- Exactly one start node:
  - `shape=Mdiamond` or id `start`
- At least one exit node:
  - `shape=Msquare` or id `exit`/`end`
- Every node must be reachable from start.
- Use semicolons after statements.
- Node IDs must match `[A-Za-z_][A-Za-z0-9_]*`.

## Supported node behaviors
- Start:
  - `shape=Mdiamond` (or `type=start`)
- Exit:
  - `shape=Msquare` (or `type=exit`)
- Tool node (shell command):
  - `shape=parallelogram` or `type=tool`
  - requires `tool_command="..."`
- Verification node (deterministic checks from plan):
  - `type=verification` (usually with `shape=parallelogram`)
  - reads plan from context key `verification.plan` by default
  - requires `verification.allowed_commands="prefix1,prefix2,..."`
- Codergen node (agent-driven):
  - default for `shape=box` (or `type=codergen`)
  - uses `prompt="..."`

## Supported edge conditions
Only these are valid in v0:
- `condition="outcome=success"`
- `condition="outcome=fail"`
- `condition="outcome=retry"`
- `condition="outcome=partial_success"`

If multiple matching edges exist, highest `weight` wins.

## Safety and guardrails
- Always set `allowed_write_paths` on executable nodes (`box`/`parallelogram`) when possible.
- `allowed_write_paths` must be comma-separated relative paths.
- Exact files are allowed by direct entry (example: `main.go`).
- Directories are allowed by trailing slash (example: `src/` allows `src/a.go`, `src/lib/b.go`, etc.).
- Absolute paths and `..` are rejected in `allowed_write_paths`.
- Tool command guardrail rejects:
  - `~`
  - `..`
  - absolute path tokens

Practical implication:
- In v0, prefer `go build .` over `go build ./...` because `./...` triggers the `..` guardrail check.

## Prompt formatting rules
- Use one quoted string for `prompt`.
- For multi-line prompts, use escaped newlines (`\n`).
- Do not rely on raw multi-line quoted strings.

Example:
```dot
prompt="Line 1\nLine 2\nLine 3\n"
```

## Recommended authoring workflow
1. Start with a minimal linear pipeline.
2. Add a test node that validates success criteria.
3. Add a fix loop for `outcome=fail`.
4. Add write guardrails (`allowed_write_paths`).
5. Run with fake backend first for deterministic flow checks.
6. Run with real backend once flow and guardrails are stable.

## Template: minimal pipeline
```dot
digraph G {
  start [shape=Mdiamond];
  work  [shape=box, prompt="Do the work.\n"];
  exit  [shape=Msquare];

  start -> work;
  work -> exit;
}
```

## Template: build/test/fix loop
```dot
digraph BuildLoop {
  start [shape=Mdiamond];
  exit  [shape=Msquare];

  generate [shape=box, allowed_write_paths="main.go,go.mod", prompt="Create code.\n"];
  test     [shape=parallelogram, tool_command="go build ."];
  fix      [shape=box, allowed_write_paths="main.go,go.mod", prompt="Fix build failures.\n"];

  start -> generate -> test;
  test -> exit [condition="outcome=success"];
  test -> fix  [condition="outcome=fail"];
  fix -> test;
}
```

## Template: codex-backed node (optional)
```dot
generate [
  shape=box,
  agent.backend="codex",
  codex.sandbox="workspace-write",
  codex.approval="never",
  codex.skip_git_repo_check=true,
  allowed_write_paths="main.go,go.mod",
  prompt="Create a minimal Go CLI.\n"
];
```

## Template: verification plan flow
```dot
digraph VerifyPlanFlow {
  start [shape=Mdiamond];
  generate [shape=box, prompt="Implement feature and return verification_plan.\n"];
  verify [shape=parallelogram, type=verification, "verification.allowed_commands"="test -f,go test,go build"];
  exit [shape=Msquare];

  start -> generate -> verify;
  verify -> exit [condition="outcome=success"];
}
```

Expected generated verification plan shape:
```json
{
  "files": ["path/to/file.go"],
  "commands": ["go test ./internal/factory"]
}
```

## Common mistakes and fixes
- Mistake: prompt written as raw multiline quote block
  - Symptom: parse error (`invalid syntax`)
  - Fix: use `\n` escaped string

- Mistake: tool command uses `./...`
  - Symptom: `tool_command rejected by guardrail: contains ..`
  - Fix: use `go build .` in v0

- Mistake: broad write scope
  - Symptom: unexpected file mutations
  - Fix: constrain with `allowed_write_paths`; prefer exact files for sensitive paths, directory entries only where needed

- Mistake: verification node has no command allowlist
  - Symptom: verification fails with `verification.allowed_commands is required`
  - Fix: set explicit command prefixes on the verification node

- Mistake: verification plan missing from context
  - Symptom: verification fails with `verification plan missing in context key`
  - Fix: ensure previous node writes `verification_plan` (or `context_updates`) to the configured context key

- Mistake: no exit path for failures
  - Symptom: routing error (`no route from node ...`)
  - Fix: add explicit fail/retry routing edges

## Validation checklist (before commit)
- DOT parses successfully.
- Graph validation passes (start/exit/reachability).
- Guardrails are defined for executable nodes.
- Test node verifies success criteria.
- Failure path exists and is intentional.
- `go test ./...` passes after any runtime changes.

# Lessons Learned

This file records concrete failure modes seen in this repo and the fixes applied.

## 1) CLI argument order is strict
- Failure mode:
  - Running `attractor run <pipeline> --workdir ... --runsdir ...` can fail with `--workdir and --runsdir are required`.
- Root cause:
  - The current CLI parses flags from `os.Args[2:]`, so flags must be placed before positional args.
- Fix:
  - Use `attractor run --workdir <path> --runsdir <path> [--run-id <id>] <pipeline.dot>`.

## 2) DOT multiline quoted prompts fail parse
- Failure mode:
  - Parser returns `invalid syntax` for raw multi-line quoted prompt values.
- Root cause:
  - Parser uses Go-style string unquoting for quoted attrs.
- Fix:
  - Encode prompts with escaped newlines (`\n`) inside a single quoted string.

## 3) Guardrail blocks `go build ./...`
- Failure mode:
  - Tool node fails with `tool_command rejected by guardrail: contains ..`.
- Root cause:
  - v0 command guardrail rejects any `..` token; `./...` contains `..` as a substring.
- Fix:
  - Use `go build .` (or other command forms that avoid `..`) in v0.

## 4) Codex in temp workspace fails trust check
- Failure mode:
  - Codex fails with `Not inside a trusted directory and --skip-git-repo-check was not specified`.
- Root cause:
  - Run workspace is an isolated temp directory without repo trust metadata.
- Fix:
  - Added config support:
    - `codex.skip_git_repo_check` (node attr)
    - `ATTRACTOR_CODEX_SKIP_GIT_REPO_CHECK` (env var)
  - Adapter now passes `--skip-git-repo-check` when enabled.

## 5) Codex response schema rejected
- Failure mode:
  - Codex API returns `invalid_json_schema` for response format.
- Root cause:
  - Schema constraints expected by Codex were stricter than initial schema assumptions.
- Fix:
  - Updated schema to satisfy current Codex requirements:
    - `context_updates` defined with `properties: {}` and `additionalProperties: false`
    - `required` includes all declared top-level properties.

## 6) Codex option placement caused unexpected sandbox behavior
- Failure mode:
  - Run showed unexpected sandbox mode despite provided config.
- Root cause:
  - Some flags were placed before `exec` rather than as subcommand options.
- Fix:
  - Build args as: `codex exec [exec options...] ...`
  - Keep global flags only where intended.

## 7) Empty workspace assumptions
- Failure mode:
  - Generated task expected `main.go`/`go.mod` to exist, but workspace was empty.
- Root cause:
  - Source workdir used for test did not contain starter files.
- Fix:
  - Prompt/spec now allows creating `main.go` and `go.mod` if absent.
  - Guardrails still constrain writes to those paths.

## Operational guidance
- Always run `go test ./...` after backend/adapter changes.
- For pipeline experiments, use `ATTRACTION_TEST_STOP_AFTER_NODE=<id>` to stop deterministically and inspect artifacts.
- Prefer explicit `allowed_write_paths` on executable nodes.

## 8) Exact-file allowlists became tedious for generated structures
- Failure mode:
  - Frequent guardrail failures when generated code added files under expected directories.
- Root cause:
  - `allowed_write_paths` originally matched only exact file paths.
- Fix:
  - Added directory allowlist entries using trailing slash syntax (example: `src/`).
- Prevention:
  - Keep sensitive files as exact entries; use directory entries only where structural flexibility is required.

## 9) Behavior checks were hard to audit when ad-hoc
- Failure mode:
  - Verification logic drifted into free-form commands without clear traceability.
- Root cause:
  - No dedicated, structured verification stage contract.
- Fix:
  - Added `type=verification` node that executes structured plan from context and requires `verification.allowed_commands`.
  - Persisted `verification.plan.json` and `verification.results.json` artifacts.
- Prevention:
  - For behavior-based validation flows, require explicit verification plan + command prefix allowlist.

## 10) Artifacts were split and hard to correlate during debugging
- Failure mode:
  - Understanding a run required manual correlation across status/events/tool logs.
- Root cause:
  - No single structured session trace covering inputs, outputs, transformations, and route choices.
- Fix:
  - Added per-run `trace.jsonl` with typed records (`NodeInputCaptured`, `NodeOutputCaptured`, `RouteEvaluated`, etc.).
- Prevention:
  - For future runtime changes, include trace record updates and tests proving trace coverage.

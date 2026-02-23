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

## 11) Agent stage appeared stalled with no visible progress
- Symptom:
  - Pipeline logs stopped at `stage started` for a Codex node with only periodic manual interruption.
  - No immediate visibility into what Codex was doing.
- Root cause:
  - Codex execution was previously awaited as a single blocking call with no lifecycle logging, no heartbeat, and no live stream surfacing.
- Fix:
  - Added structured Codex lifecycle logs:
    - `codex exec started`
    - `codex exec still running` (heartbeat)
    - `codex exec completed` / `codex exec failed` / `codex exec timed out`
  - Added timeout controls:
    - node attr `codex.timeout_seconds`
    - env `ATTRACTOR_CODEX_TIMEOUT_SECONDS`
  - Added heartbeat controls:
    - node attr `codex.heartbeat_seconds`
    - env `ATTRACTOR_CODEX_HEARTBEAT_SECONDS`
  - Added live stream option:
    - `FACTORY_LOG_CODEX_STREAM=1`
  - Added persistent run artifacts:
    - `<node>/codex.args.txt`
    - incremental `<node>/codex.stdout.log` and `<node>/codex.stderr.log`
- Prevention (test/check/guardrail):
  - For agent backend changes, require end-to-end execution that verifies node lifecycle logs and presence of codex artifact files.
  - Avoid interrupting runs before timeout/exit when diagnosing backend behavior.

## 12) Recursive workspace copy when `runsdir` is under `workdir`
- Symptom:
  - Run failed during workspace preparation with repeated nested `.runs/.../workspace` path growth and `file name too long`.
- Root cause:
  - Workspace copy included `runsdir` when `runsdir` was inside `workdir`, causing recursive self-copy.
- Fix:
  - Copy step now auto-excludes nested runsdir relative path when `runsdir` is a descendant of `workdir`.
  - Added regression test `TestWorkspaceCopyExcludesNestedRunsDir`.
- Prevention (test/check/guardrail):
  - Keep using `go test ./...` as commit gate for engine changes.
  - Prefer external runs directory in local usage, but nested runsdir is now safe.

## 13) Fix loops can persist when validation fails due external provider config
- Symptom:
  - Pipeline repeatedly cycled `validate_user_scenarios -> fix -> validate_component -> validate_user_scenarios`.
- Root cause:
  - Validation failure came from external provider/model configuration (HTTP 404 model not found), not code defects in `agent/`.
  - `fix` node was restricted to `allowed_write_paths="agent/"`, so it could not remediate scenario/config failures.
- Fix:
  - Added shared scenario preflight harness `scripts/scenarios/preflight_scenario.sh`.
  - Standardized user scenarios on `SCENARIO_MODE=selftest|live`:
    - `selftest` for deterministic script logic.
    - `live` for real API/dependency checks.
- Prevention (test/check/guardrail):
  - Run selftest first to detect scenario bugs early.
  - Classify live failures as external/config issues when code checks already pass.
  - Parameterize live model IDs via env vars to avoid hardcoded unavailable models.

## 14) Agent overfit risk when scenario scripts are visible
- Symptom:
  - Codex nodes read and executed `scripts/scenarios/agent_cli_user_scenarios.sh` directly, so holdout behavior checks were effectively visible to the builder agent.
- Root cause:
  - Codex ran with workspace root as CWD and unrestricted workspace visibility for reads.
  - Prompts explicitly told the agent to read scenario scripts.
- Fix:
  - Added Codex runtime read-isolation:
    - default hidden path `scripts/scenarios/` during Codex execution
    - optional overrides via `codex.block_read_paths` / `ATTRACTOR_CODEX_BLOCK_READ_PATHS`
    - opt-out via `codex.allow_read_scenarios=true`
  - Updated pipeline node config to:
    - `codex.workdir="agent"`
    - `codex.add_dirs="examples/specs"`
  - Updated prompts to use only spec input and explicitly avoid scenario scripts.
- Prevention (test/check/guardrail):
  - Treat scenario scripts as external validators, not implementation context.
  - For any new pipeline, set minimal Codex visibility (`workdir` + explicit `add_dirs`) before running long fix loops.

## 15) Guardrail failures caused by Go cache placement
- Symptom:
  - `implement` stage failed with `guardrail_violation` listing thousands of `.gocache/...` files.
- Root cause:
  - Codex executed Go commands from workspace root with `GOCACHE="$PWD/.gocache"`, creating cache outside `allowed_write_paths="agent/"`.
- Fix:
  - Run Codex from `agent/` and require `GOCACHE="$PWD/.gocache"` in agent prompts so cache stays under `agent/.gocache`.
- Prevention (test/check/guardrail):
  - Keep cache/temp outputs within explicitly allowed directories.
  - Prefer tightening execution directory over broadening `allowed_write_paths`.

## 16) Verification plan commands failed allowlist due shell wrappers
- Symptom:
  - `verify_plan` failed with `verification command not allowed: GOCACHE="..." go test ./...`.
- Root cause:
  - Allowlist matching previously required literal command prefix at position 0.
  - Env-prefix and wrapper forms (`GOCACHE=...`, `cd ... &&`, `export ... &&`) were not normalized.
- Fix:
  - Normalized verification commands before allowlist checks to match effective command intent.
  - Added integration and smoke coverage for env-prefixed `go test`.
- Prevention (test/check/guardrail):
  - Keep command allowlist policy focused on effective command families (`go test`, `go build`, `gofmt`).
  - Run `scripts/smoke_verification_allowlist.sh` in CI.

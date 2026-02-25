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

## 17) Builder spec leaked orchestration context and biased implementation
- Symptom:
  - Agent logs showed full orchestrator/layer context being read from builder-visible spec.
- Root cause:
  - `examples/specs/agent_cli_poc_spec.md` mixed product requirements with factory orchestration description.
- Fix:
  - Added a dedicated product-only builder spec:
    - `examples/specs/agent_cli_builder_spec.md`
  - Updated builder/fix prompts to read only builder spec.
  - Explicitly instructed agent not to rely on orchestrator/pipeline metadata.
- Prevention (test/check/guardrail):
  - Keep builder-visible specs free of orchestration internals.
  - Keep orchestration docs in separate files not referenced by builder prompts.

## 18) Builder node still read memory docs via parent traversal
- Symptom:
  - Codex logs showed reads of `../ARCHITECTURE.md`, `../DECISIONS.md`, `../LESSONS_LEARNED.md`, etc., despite using a builder-only spec.
- Root cause:
  - `codex.workdir="agent"` + `codex.add_dirs="examples/specs"` limited intended context but did not hard-restrict parent path reads.
- Fix:
  - Added `codex.strict_read_scope` runtime behavior to hide all workspace entries except workdir + add_dirs during Codex execution.
  - Enabled `codex.strict_read_scope=true` in the Agent CLI POC implement/fix nodes.
- Prevention (test/check/guardrail):
  - For builder-isolated pipelines, always set `codex.strict_read_scope=true`.
  - Keep builder specs and orchestrator docs separated, then enforce separation with strict scope.

## 19) Verification command cwd mismatch caused repeated fix loops
- Symptom:
  - `verify_plan` repeatedly failed on commands like `gofmt -w main.go main_test.go` with exit code 2.
- Root cause:
  - Verification executed commands at workspace root, while generated commands assumed `agent/` cwd.
- Fix:
  - Added `verification.workdir` and set it to `agent` in the Agent CLI POC pipeline.
  - Added integration test coverage for configured verification workdir behavior.
- Prevention (test/check/guardrail):
  - Set explicit `verification.workdir` when command paths are app-relative.

## 20) Fix loops persist when failure details are not passed into fix prompts
- Symptom:
  - Pipeline repeatedly cycled `validate_user_scenarios -> fix` while the fix agent changed unrelated code.
- Root cause:
  - Runtime only routed on `outcome=fail` and `failure_reason=tool_exit_code_1`.
  - Concrete failing details (for example model 404 in scenario stderr) were not injected into the next codergen prompt.
- Fix:
  - On failed stages, runtime now stores structured context under `last_failure.*`.
  - Codergen prompts automatically append a `Failure feedback` section sourced from that context.
  - Added tests that assert failure context storage and fix prompt injection.
- Prevention (test/check/guardrail):
  - Treat artifact capture and prompt feedback plumbing as first-class orchestration behavior.
  - Keep failure summaries concise and bounded to prevent prompt bloat.

## 21) Static live model ids in scenarios cause persistent false failures
- Symptom:
  - `validate_user_scenarios` repeatedly failed with Anthropic 404 for specific model ids.
- Root cause:
  - `agent_cli_user_scenarios.sh` used a hardcoded default Anthropic live model id.
  - Provider model availability changed and differed from preflight model resolution.
- Fix:
  - Resolve Anthropic model dynamically from `/v1/models` when not explicitly configured.
  - Replace fixed `/tmp` artifact names with `mktemp` outputs for safer parallel execution.

## 22) Relative `codex.path` fails when wrapper is missing from workdir
- Symptom:
  - Implement/fix stage failed with `fork/exec .../.factory/bin/codex: no such file or directory`.
- Root cause:
  - Pipeline configured `codex.path=".factory/bin/codex"` but wrapper did not exist in source `--workdir`, so it was absent in copied run workspace.
- Fix:
  - Added explicit `ensure_codex_wrapper` tool stage to create `.factory/bin/codex` before Codex nodes run.
  - Added fail-fast executable validation in Codex adapter with clear error text for missing/non-executable `codex.path`.
- Prevention (test/check/guardrail):
  - For relative `codex.path`, include a bootstrap stage or pre-create wrapper in workdir.
  - Keep unit coverage for missing executable error path.

## 23) Verification allowlist could be bypassed via shell chaining
- Symptom:
  - Commands like `go test ./...; <extra>` could satisfy prefix allowlist intent and still execute additional shell actions.
- Root cause:
  - Verification executed commands through `sh -c`; allowlist checked only the command prefix.
- Fix:
  - Verification now rejects unsafe shell syntax (`;`, `&&`, `||`, pipes, redirects, subshell markers).
  - Verification now executes parsed commands directly (with controlled leading env assignments), not via `sh -c`.
- Prevention (test/check/guardrail):
  - Keep negative tests for shell-chain rejection.
  - Keep end-to-end negative run proving unsafe verification commands fail.

## 24) Strict read scope leaked sibling paths under allowed roots
- Symptom:
  - With `codex.add_dirs="examples/specs"`, Codex could still read other files under `examples/`.
- Root cause:
  - Strict read scope originally preserved top-level roots, not exact allowlisted subpaths.
- Fix:
  - Strict scope now hides non-allowlisted subpaths recursively; only explicit path prefixes remain readable.
- Prevention (test/check/guardrail):
  - Keep tests that assert `examples/specs` is readable while `examples/other` is blocked.
  - Keep executable path (`codex.path`) explicitly included in strict-scope keep set.
- Prevention (test/check/guardrail):
  - Avoid hardcoded live model defaults in scenario scripts.
  - Require scenario scripts to use dynamic provider model discovery or explicit env override.

## 22) Scenario bugs need fail-fast lint, not runtime discovery
- Symptom:
  - Scenario incompatibilities were discovered only after long implement/fix cycles.
- Root cause:
  - No hard preflight guardrail validating scenario-script contracts before pipeline execution.
- Fix:
  - Added `lint_scenarios.sh` and wired it as the first pipeline gate.
  - Added tests for linter pass/fail behavior.
- Prevention (test/check/guardrail):
  - Keep scenario lint as required preflight for scenario-driven pipelines.

## 23) Some failures are unfixable under current write scope
- Symptom:
  - Pipeline loops on fix even though failure source belongs to scenario tooling outside app write scope.
- Root cause:
  - Routing sent failures to a fix node that was not permitted to modify the failing source files.
- Fix:
  - Added `unfixable_failure_source` runtime guardrail for codergen nodes.
  - Guardrail compares failed tool script paths with fix node `allowed_write_paths`.
- Prevention (test/check/guardrail):
  - Keep fix node write scope aligned with expected failure-source ownership.
  - Fail fast when scopes are incompatible.

## 24) Live scenario failures need explicit classification
- Symptom:
  - Tool failures from live scenario checks looked identical (`tool_exit_code_1`) regardless of root cause.
- Root cause:
  - Preflight wrapper did not classify provider/config failures separately from product behavior failures.
- Fix:
  - Added live failure classification to `preflight_scenario.sh`:
    - `failure_class=infra` with dedicated exit code `86`
    - `failure_class=product` with original exit code
- Prevention (test/check/guardrail):
  - Keep classification patterns up to date with provider error signatures.
  - Add tests for infra/product classification behavior.

## 25) Global Codex MCP memory can leak guidance into factory runs
- Symptom:
  - Codex in pipeline runs attempted to search for repo memory docs despite builder prompts not requesting them.
- Root cause:
  - Codex subprocess inherited global MCP server config from `~/.codex/config.toml`.
- Fix:
  - Added local executable path support (`codex.path`) and MCP disable switch (`codex.disable_mcp`).
  - Factory now supports wrapper-based Codex invocation with MCP disabled for isolated runs.
- Prevention (test/check/guardrail):
  - Use local Codex wrapper path + `codex.disable_mcp=true` for factory pipelines.
  - Keep MCP-off behavior covered by unit tests in agent option/arg construction.

## 26) Local tool wrappers fail if workspace copy strips executable bits
- Symptom:
  - Codex stage failed with `permission denied` when using `codex.path` wrapper inside workspace.
- Root cause:
  - `copyDir` wrote copied files with fixed `0644`, removing executable bits.
- Fix:
  - Preserve source file permissions during workspace copy.
  - Added test coverage for executable-bit preservation.
- Prevention (test/check/guardrail):
  - Keep file-mode preservation in workspace-copy tests.
  - Include at least one e2e run using local executable path wrappers.

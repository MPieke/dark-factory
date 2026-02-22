# Dark Factory (Attractor v0)

`dark-factory` is a Go CLI that executes a small pipeline graph described in DOT.

Based on https://github.com/strongdm/attractor/

At a high level, it:
- Parses and validates a `digraph` pipeline.
- Creates an isolated run workspace by copying your source workdir.
- Executes nodes in graph order (with conditional routing and retries).
- Writes run artifacts (`manifest`, `events`, per-node status/output, checkpoints).
- Supports resume from a prior run checkpoint.

## What this repo contains

- `cmd/attractor/main.go`: CLI entrypoint.
- `internal/attractor/parser.go`: DOT parser.
- `internal/attractor/validate.go`: graph validation and guardrails.
- `internal/attractor/engine.go`: run engine, handlers, artifacts, resume logic.
- `internal/attractor/*_test.go`: unit tests.
- `scripts/smoke.sh`: end-to-end smoke script.

## How to use

## 1) Run tests

```bash
go test ./...
```

## 2) Build the CLI

```bash
go build -o ./bin/factory ./cmd/factory
```

## 3) Create a pipeline file

Example `pipeline.dot`:

```dot
digraph G {
  graph [goal="generate a release note"];

  start [shape=Mdiamond];
  generate [shape=box, type=codergen, prompt="Draft output for: $goal"];
  check [shape=parallelogram, type=tool, tool_command="test -f README.md"];
  exit [shape=Msquare];

  start -> generate;
  generate -> check [condition="outcome=success"];
  generate -> exit  [condition="outcome=fail"];
  check -> exit;
}
```

## 4) Run a pipeline

```bash
./bin/factory run pipeline.dot --workdir . --runsdir ./runs --run-id demo
```

Required flags:
- `--workdir`: source directory copied into the run workspace.
- `--runsdir`: parent directory where run artifacts are stored.

Optional flags:
- `--run-id`: explicit run id (otherwise current UTC timestamp is used).
- `--resume`: resume an existing run (requires `--run-id`).

## 5) Inspect outputs

For run id `demo`, artifacts are in `runs/demo/`:
- `manifest.json`: run metadata.
- `events.jsonl`: pipeline/stage lifecycle events.
- `checkpoint.json`: resume state.
- `<node-id>/status.json`: node outcome.
- `<node-id>/prompt.md`, `response.md`: codergen node inputs/outputs.
- `<node-id>/tool.stdout.txt`, `tool.stderr.txt`, `tool.exitcode.txt`: tool node command output.
- `<node-id>/workspace.diff.json`: file changes made during node execution.
- `workspace/`: copied workdir used for this run.

## Node behavior summary

Node handler selection:
- `shape=Mdiamond` or `type=start` -> start handler.
- `shape=Msquare` or `type=exit` -> exit handler.
- `shape=parallelogram` or `type=tool` -> tool handler.
- default (`shape=box` / unspecified type) -> codergen handler.

Supported edge conditions:
- `outcome=success`
- `outcome=fail`
- `outcome=retry`
- `outcome=partial_success`

If multiple matching edges exist, highest `weight` wins.

## Fake backend mode (useful for tests)

Set `ATTRACTION_BACKEND=fake` (or `ATTRACTOR_BACKEND=fake`) to make `codergen` nodes return deterministic outcomes from test attrs (for example `test.outcome`, `test.outcome_sequence`).

Example:

```bash
ATTRACTION_BACKEND=fake ./bin/factory run pipeline.dot --workdir . --runsdir ./runs --run-id fake-demo
```

## Codex backend (real agent execution)

`codergen` nodes can run through a pluggable agent interface. The built-in real backend is `codex`.

Enable it:

```bash
ATTRACTOR_AGENT_BACKEND=codex ./bin/factory run pipeline.dot --workdir . --runsdir ./runs --run-id codex-demo
```

You can configure Codex at node level (`codex.*` attrs) or via env vars:

- Sandbox:
  - attr: `codex.sandbox`
  - env: `ATTRACTOR_CODEX_SANDBOX`
- Approval policy:
  - attr: `codex.approval`
  - env: `ATTRACTOR_CODEX_APPROVAL`
- Working directory:
  - attr: `codex.workdir` (relative to run workspace, or absolute)
  - env: `ATTRACTOR_CODEX_WORKDIR`
- Additional writable directories:
  - attr: `codex.add_dirs` (CSV)
  - env: `ATTRACTOR_CODEX_ADD_DIRS` (CSV)
- Auto-approved command list via config override key:
  - attr: `codex.auto_approve_commands` (CSV)
  - attr: `codex.auto_approve_config_key` (target Codex config key)
  - env: `ATTRACTOR_CODEX_AUTO_APPROVE_COMMANDS`, `ATTRACTOR_CODEX_AUTO_APPROVE_CONFIG_KEY`
- Raw Codex config overrides:
  - attr: `codex.config_overrides` (`;;`-separated `key=value` entries)
  - env: `ATTRACTOR_CODEX_CONFIG_OVERRIDES`
- Optional model/profile:
  - attr: `codex.model`, `codex.profile`
  - env: `ATTRACTOR_CODEX_MODEL`, `ATTRACTOR_CODEX_PROFILE`

Codex outputs and schema are written per node:
- `<node>/codex.output.schema.json`
- `<node>/codex.stdout.log`
- `<node>/codex.stderr.log`
- `<node>/response.md` (JSON response mapped to stage outcome)

## Smoke script

Run the included smoke test:

```bash
bash scripts/smoke.sh
```

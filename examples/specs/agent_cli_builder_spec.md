# Agent CLI Builder Spec

## Goal
Create a CLI application in `agent/` that can call provider backends, with cheap-model defaults and deterministic mock mode.

## CLI requirements
- Command:
  - `agent-cli ask --provider <openai|anthropic> [--model <model>] --prompt <text> [--mock]`
- Cheap model defaults:
  - `openai`: `gpt-4.1-mini`
  - `anthropic`: `claude-3-5-haiku-latest`
- In `--mock` mode:
  - do not call network APIs
  - print JSON with fields:
    - `provider`
    - `model`
    - `prompt`
    - `response`
  - `response` is deterministic, for example:
    - `mock:<provider>:<model>:<prompt>`
- Unsupported provider must fail with non-zero exit code.

## Constraints
- Only modify files under `agent/`.
- Do not modify files outside `agent/`.
- Code must compile and tests must pass.

# Agent Guide

Use these docs as the primary context before making code changes:

1. `ARCHITECTURE.md`
   - System structure, runtime flow, core components, and artifact model.
2. `DECISIONS.md`
   - Rationale for logic choices (guardrails, permissions, retries, routing, and backend behavior).
3. `TESTING_STRATEGY.md`
   - Spec-first and test-first execution policy, anti-reward-hacking checks, and autonomous validation rules.

## Working rules for agents in this repo
- Preserve v0 constraints unless explicitly asked to expand scope.
- Keep routing deterministic and testable.
- Treat guardrails (`allowed_write_paths`, tool command checks) as security boundaries; do not weaken silently.
- If behavior changes, update both `ARCHITECTURE.md` and `DECISIONS.md` in the same change.
- Follow `TESTING_STRATEGY.md`:
  - define spec first
  - add/update tests before implementation changes
  - prefer AI-executable validation over manual review
- Add or update tests in `internal/attractor/*_test.go` for runtime or validation behavior changes.

# Agent Guide

Use these docs as the primary context before making code changes:

1. `ARCHITECTURE.md`
   - System structure, runtime flow, core components, and artifact model.
2. `DECISIONS.md`
   - Rationale for logic choices (guardrails, permissions, retries, routing, and backend behavior).
3. `TESTING_STRATEGY.md`
   - Spec-first and test-first execution policy, anti-reward-hacking checks, and autonomous validation rules.
4. `LESSONS_LEARNED.md`
   - Prior failure modes, root causes, and approved fixes to avoid repeating mistakes.
5. `DEVELOPER_GUIDELINES.md`
   - Code quality, module/package structure, and branch/PR workflow standards.

## Working rules for agents in this repo
- Preserve v0 constraints unless explicitly asked to expand scope.
- Keep routing deterministic and testable.
- Treat guardrails (`allowed_write_paths`, tool command checks) as security boundaries; do not weaken silently.
- If behavior changes, update both `ARCHITECTURE.md` and `DECISIONS.md` in the same change.
- Follow `TESTING_STRATEGY.md`:
  - define spec first
  - add/update tests before implementation changes
  - prefer AI-executable validation over manual review
- Follow `DEVELOPER_GUIDELINES.md` for code structure, module boundaries, and git/PR workflow.
- Check `LESSONS_LEARNED.md` before implementing similar flows; reuse known fixes unless requirements changed.
- Add or update tests in `internal/attractor/*_test.go` for runtime or validation behavior changes.

## Memory maintenance rules
- Treat markdown docs as persistent operational memory. Keep them current in the same PR/commit as code changes.
- Update docs when any of these occur:
  - behavior or interface change
  - bug/incident/root-cause discovery
  - new guardrail, permission, sandbox, directory, or provider configuration behavior
  - repeated developer/operator confusion that should become explicit guidance
- Update the right file(s):
  - `ARCHITECTURE.md`: what changed in structure/runtime flow/components/artifacts
  - `DECISIONS.md`: why the choice was made and tradeoffs
  - `TESTING_STRATEGY.md`: validation policy/process changes
  - `LESSONS_LEARNED.md`: concrete failure mode and fix
  - `README.md`: user-facing usage changes only
- For `LESSONS_LEARNED.md`, use this template:
  - Symptom
  - Root cause
  - Fix
  - Prevention (test/check/guardrail)
- Be specific:
  - include exact flags/env vars/commands/file paths
  - avoid vague statements without actionable detail
- Commit-time gate:
  - if behavior changed but docs were not updated, work is incomplete
  - if lesson-worthy failure occurred and no `LESSONS_LEARNED.md` entry was added, work is incomplete

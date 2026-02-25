# Testing Strategy

This repo uses a spec-first, test-first approach with automation as the default validator.

## Core policy
- Write or update executable tests before behavior code changes.
- Define clear acceptance specs first, then implement to satisfy those specs.
- Prefer machine-verifiable checks over human judgment whenever possible.
- Keep humans out of the loop unless requirements conflict or results are ambiguous.

## Delivery loop (required)
1. Define spec:
   - Expected inputs, outputs, side effects, and failure modes.
   - Deterministic outcomes and artifact expectations.
2. Write tests:
   - Unit tests for logic branches and edge cases.
   - Guardrail tests for unsafe/path-escape behavior.
   - End-to-end tests for full pipeline execution and artifacts.
3. Implement:
   - Minimal change that satisfies tests/spec.
4. Validate:
   - Run full suite (`go test ./...`).
   - Treat test failures as blocking.
5. Harden:
   - Add regression tests for each bug found.

## Test pyramid in this repo
- Unit tests:
  - Parser/value coercion
  - Graph validation/routing
  - Agent config parsing and command construction
- Integration tests:
  - Handler execution with workspace diffs
  - Workspace copy mode preservation (executable-bit retention)
  - Retry/checkpoint semantics
  - Guardrail enforcement
  - Failure-feedback propagation (`last_failure.*` context + fix prompt injection)
  - Unfixable-scope guardrail (`unfixable_failure_source`) behavior
- End-to-end tests:
  - Full `RunPipeline` flows and run artifact verification
  - Smoke validation through `scripts/smoke.sh`
  - Verification command allowlist smoke through `scripts/smoke_verification_allowlist.sh`
  - Scenario guardrail lint checks through `scripts/scenarios/lint_scenarios.sh`

## Anti-reward-hacking guardrails
- Never “test only the happy path” when adding capabilities.
- For each success path, include at least one adversarial or misuse case.
- Assert externally visible outcomes, not internal implementation details alone.
- Use negative tests that prove failures occur when constraints are violated.
- Do not relax validations to make tests pass; fix behavior or tighten specs.

## Autonomous validation policy
- If an AI can validate the result with executable checks, default to AI validation.
- Escalate to human review only when:
  - specs are incomplete or contradictory
  - test evidence conflicts
  - safety or policy intent is unclear

## Scenario preflight policy
- Scenario scripts should implement `SCENARIO_MODE=selftest|live`.
- `selftest` must not require external services and should fail only for scenario logic defects.
- `live` validates real external behavior (for example provider API calls) and can fail for runtime/config issues.
- Live scenario scripts should resolve provider models dynamically when possible; avoid hardcoded model defaults.
- Prefer running scenarios through the shared harness:
  - `bash scripts/scenarios/preflight_scenario.sh <scenario_script> <app_dir>`
- Preflight harness should classify live failures (`infra` vs `product`) and expose that classification in output.
- Use `REQUIRE_LIVE=0` only when intentionally skipping external dependency checks.

## Spec quality requirements
Specs should be precise enough that two independent implementations converge:
- Explicit allowed and disallowed behavior
- Concrete examples with expected outputs
- Clear error conditions
- Deterministic tie-break/ordering rules

## Done criteria
A change is done only when:
- Spec exists and matches behavior.
- Tests were added/updated first (or in same change before final implementation step).
- All tests pass locally.
- New behavior has regression coverage.

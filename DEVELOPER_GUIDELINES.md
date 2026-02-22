# Developer Guidelines

This file defines coding and delivery standards for this repository.

## Source of truth
- `TESTING_STRATEGY.md` is the required testing policy for all behavior changes.

## MUST rules
- Follow `TESTING_STRATEGY.md` for spec-first, test-first, and validation steps.
- Keep modules focused; split code when unrelated responsibilities accumulate.
- Keep package boundaries explicit and simple to avoid hard-to-manage imports.
- Use a new branch for each feature, fix, or refactor.
- Open PRs to `main` by default.
- Do not merge directly to `main` unless explicitly instructed.
- Add/update tests for all behavior changes and bug fixes.
- Update relevant docs in the same change when behavior or decisions change.

## SHOULD rules
- Group code by functionality (vertical slices), not by generic code type.
- Keep cross-cutting infrastructure isolated (config/logging/common tooling).
- Prefer small, composable functions with clear inputs/outputs.
- Keep error messages actionable and contextual.
- Keep commits logically scoped and reviewable.

## Directory and packaging guidance
- Prefer domain-oriented directories (example: `internal/attractor/<feature-area>`).
- Avoid deep package hierarchies unless they simplify ownership and imports.
- If a package has too many reasons to change, split by functional boundary.
- Place tests near the functionality they validate.

## Git workflow
- Branch naming:
  - `feature/<short-name>`
  - `fix/<short-name>`
  - `chore/<short-name>`
- Before push:
  - run `go test ./...`
  - review diff for accidental scope creep
  - confirm docs were updated if required
- PR target:
  - `main` unless otherwise directed

# Budgetctl Builder Spec

## Goal
Create a Go CLI in `budgetctl/` named `budgetctl`.

## Required command
- `budgetctl run --tx <transactions.csv> --rules <rules.csv> --out <report.json>`

## Behavior
- Parse transactions CSV with header: `id,date,description,amount,currency,merchant,account`.
- Parse rules CSV with header: `pattern,category`.
- Match rule when lowercase `pattern` is a substring of lowercase `merchant` OR `description`.
- First matching rule wins.
- If no rule matches, category is `Uncategorized`.
- Output report JSON with fields:
  - `transactions`: array with `id`, `merchant`, `description`, `amount`, `category`
  - `summary.category_totals`: map category -> numeric total (sum of amount)
  - `summary.uncategorized_count`: integer
- Preserve input row order in `transactions` output.

## Constraints
- Only modify files under `budgetctl/`.
- Add unit tests for rule priority, fallback category, and deterministic ordering.
- Ensure `go test ./...` and `go build ./...` succeed from `budgetctl/`.

# Budgetctl Hypotheses

## Product under test
`budgetctl` CLI:
- `budgetctl run --tx <transactions.csv> --rules <rules.csv> --out <report.json>`

`rules.csv` schema:
- `pattern,category`

## Hypotheses
- `H1_spec_priority`
  - If multiple rules match, first rule wins deterministically.
  - Acceptance: visible scenarios show first-match behavior and stable output order.

- `H2_spec_fallback`
  - Unmatched transactions become `Uncategorized`.
  - Acceptance: visible + hidden scenarios show fallback count > 0 when expected.

- `H3_transfer_holdout`
  - Passing visible scenarios transfers to hidden fixtures with different merchants/order.
  - Acceptance: hidden scenario status success.

- `H4_robust_input`
  - App fails clearly on malformed input and succeeds on reordered benign input.
  - Acceptance: independent probe passes both negative and benign-perturbation checks.

- `H5_non_gameable`
  - Builder cannot read holdout scripts directly.
  - Acceptance: codex strict scope + default scenario blocking enabled in pipeline.

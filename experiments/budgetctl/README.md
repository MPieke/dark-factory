# Budgetctl Empirical Cycle

This experiment exercises a full hypothesis cycle:
- hypothesis-driven build (`examples/specs/budgetctl_builder_spec.md`)
- visible validators
- hidden holdout validators
- independent real-app probe
- report generation

## Run
```bash
bash experiments/budgetctl/run_empirical_cycle.sh
```

Optional overrides:
- `RUN_ID=<id>`
- `WORK=<workdir>`
- `RUNS=<runsdir>`
- `MAX_FACTORY_RETRIES=<n>`
- `ATTRACTOR_AGENT_BACKEND=codex|fake`
- `FACTORY_LOG_CODEX_STREAM=1`
- `FACTORY_API_URL=http://127.0.0.1:8080` (submit run via API instead of local CLI)
- `FACTORY_API_WORKDIR=/workspace`
- `FACTORY_API_RUNSDIR=/workspace/.runs`

## Fundamental caveat
Hidden holdouts lose value if detailed hidden failure text is repeatedly fed back to fix prompts. Keep hidden checks coarse and rotate fixtures.

# Hypothesis Protocol

This protocol defines a repeatable empirical loop for factory-built software that minimizes reward hacking and Goodhart failures.

## Objective
A run is only considered successful when all of these hold:
- visible checks pass
- hidden holdout checks pass
- independent real-app probes pass
- no guardrail/security violations occur

## Hypothesis taxonomy
- `H_spec`: visible spec checks map to intended behavior.
- `H_transfer`: passing visible checks transfers to hidden holdout scenarios.
- `H_robust`: behavior is stable under benign perturbations and misuse inputs.
- `H_observe`: artifacts explain failures well enough for deterministic repair.
- `H_non_gameable`: implementation cannot overfit evaluator internals.

## Experimental cycle
1. Define hypotheses with measurable acceptance criteria.
2. Define visible and hidden scenarios from hypotheses.
3. Run factory pipeline.
4. Run independent real-app probes outside factory verifier path.
5. Score outcomes and classify failures:
   - `spec_gap`
   - `test_gap`
   - `orchestrator_gap`
   - `implementation_bug`
   - `model_or_backend_limit`
6. Refine hypotheses and repeat.

## Anti-Goodhart requirements
- Keep hidden scenarios out of builder-visible context.
- Enforce command allowlists in verification.
- Include adversarial/negative checks for each success path.
- Require at least one independent probe not used as a factory validator.

## Holdout leakage warning
Hidden scenarios can leak over repeated fix loops if failure feedback includes detailed holdout assertions. To preserve holdout value:
- return coarse failure classes from hidden checks in early loops
- rotate hidden fixtures periodically
- keep independent probe set separate from hidden validator set

## Report schema (minimum)
- run metadata (pipeline, backend, commit, timestamp)
- hypothesis scores (pass/fail)
- scenario outcomes (visible/hidden/probe)
- failure classification and root cause
- next refinement actions

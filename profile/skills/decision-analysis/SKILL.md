---
name: decision-analysis
description: Compare options with explicit criteria and trade-offs.
version: 0.1.0
platforms: [macos, linux]
required_environment_variables: []
required_credential_files: []
metadata:
  hermes:
    tags: [personal-os, decisions, analysis]
    category: reasoning
---

# Decision Analysis

## When to Use

Use when a meaningful choice has multiple options, constraints, and explicit
trade-offs.

## Procedure

1. State the question, options, criteria, and the 0-10 meaning of each score.
2. Put weights and scores in the JSON shape accepted by
   `scripts/score_options.py`.
3. Run the helper and inspect missing scores, risks, and ties.
4. Record the reasoning and recommendation; the person makes the decision.

Weights may be any positive 0-10 values. Missing scores are shown and treated
as zero so an incomplete matrix cannot look complete.

## Pitfalls

- Do not manufacture scores from vague prose.
- Do not optimize a proxy criterion while ignoring constraints.
- A top score is not permission to spend, send, schedule, or trade.

## Verification

Confirm every criterion is visible, missing evidence is disclosed, and the
recommendation has been reviewed against qualitative risks.

---
name: deep-research
description: Build an evidence matrix and bounded research brief.
version: 0.1.0
platforms: [macos, linux]
required_environment_variables: []
required_credential_files: []
metadata:
  hermes:
    tags: [personal-os, research, evidence]
    category: research
---

# Deep Research

## When to Use

Use for a question that needs multiple sources, explicit claims, and visible
uncertainty rather than a quick answer.

## Procedure

1. Define the question, scope, and what would change the conclusion.
2. Collect sources with the available read-only research tools and record each
   source title, URL, notes, and 0-5 evidence dimensions.
3. Run `scripts/research_matrix.py INPUT.json` to rank supplied notes.
4. Write a brief with claims, evidence, counterevidence, gaps, and next checks.

The helper is deliberately offline: it reads researcher-supplied metadata and
never fetches URLs, follows links, or stores credentials.

## Pitfalls

- Do not treat source count as evidence quality.
- Separate a source's claim from the source's authority.
- Cite uncertainty and avoid presenting a provisional result as fact.

## Verification

Confirm each important claim has a source trail, gaps are visible, and the
matrix says when it relied on incomplete dimensions.

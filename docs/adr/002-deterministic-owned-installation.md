# ADR 002: Deterministic plans and ownership-limited effects

## Status

Selected implementation default for this plan; not separately founder-ratified.

## Date

2026-09-28

## Context

A model-tailored setup must not make arbitrary installation text executable or overwrite an existing development environment.

## Decision

LLM output selects validated catalog IDs. A pure planner produces the exact ordered operation list and digest. Participant approval binds to that digest. The executor rechecks preimages, locks roots, journals effects and records ownership. Unknown recipes become manual guidance. File/key rollback is conditional on unchanged postimages; system installs are reconciled rather than claimed transactional.

## Consequences

New recipes need qualification and maintenance. Recovery is more complex than running a shell script, but failures become observable. User edits and preexisting tools survive update/removal. Script generation by the model is excluded from execution.

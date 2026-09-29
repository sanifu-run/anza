---
name: anza-plan
description: Use when a project request needs several steps, has dependencies, or needs a reviewable implementation plan. Make work order, ownership, checks, and permission gates explicit without requiring a particular agent framework.
---

# Plan reviewable project work

Use this skill after the desired outcome is understood and before a multi-step change. For a small, independent edit, a short checklist may be enough.

## Inputs

- An agreed outcome, acceptance check, and scope boundaries.
- Relevant repository instructions and current project state, when available.
- Known constraints, dependencies, available people or tools, and actions that need permission.

## Workflow

1. Break the outcome into the smallest useful work items. Order items by dependency, not by convenience.
2. Give each item a concrete result, an owner when ownership matters, and a check that can show whether it is done.
3. Mark assumptions, external dependencies, irreversible effects, and permission gates. Keep work that does not depend on a gate moving when practical.
4. Keep ownership narrow. Identify files, systems, or decisions that should remain untouched, and preserve existing changes.
5. Include a useful first slice that can be reviewed early. Avoid planning speculative cleanup or work that does not support the outcome.
6. Invite correction when a dependency, order, or scope choice materially affects the participant's goal.

## Output

Return a concise plan containing the outcome, ordered work items, dependencies, owners if needed, acceptance evidence, assumptions, and gates. For each gate, state what information or authorization unlocks it and what can proceed meanwhile.

## Check completion

Every required outcome has at least one work item and an observable check. Dependencies point to work that actually unlocks them. No item assumes an unprovided tool, account, approval, or successful result. The participant can review the plan before changes with meaningful side effects begin.

## Continue

Record progress against the plan as evidence appears. If new information changes the dependency order or acceptance check, explain the adjustment and keep the remaining work bounded. A plan is not evidence that its steps have been completed.

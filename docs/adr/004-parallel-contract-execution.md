# ADR 004: Bounded parallel execution from frozen contracts

## Status

Accepted planning approach at founder request; execution certification pending.

## Date

2026-09-28

## Context

The founder requested an exhaustive prescriptive plan executable in parallel by GPT-6-Luna. Agents sharing schemas, manifests and command wiring can otherwise create incompatible implementations.

## Decision

Provide 53 task contracts across Anza and chat, frozen domain/API/CLI contract v1 revision 2, repository-qualified ownership, dependency graph and a conservative three-worker schedule plus coordinator. The coordinator is sole writer of Anza planning records and integrates cross-repository interfaces. Chat work is reconciled with its current owner; workers use repository-specific isolated worktrees. Explicitly revalidate detailed later contracts before dispatch. Avoid competing file ownership within a wave. Tests use real local boundaries with injected external providers; runtime/native/live qualification is separate.

## Consequences

There is an intentional serialized foundation and integration spine. Concurrency is bounded by runtime capacity and build leases. Structural validity is not Luna certification. A failed/ambiguous contract is amended rather than silently delegated to a different model or marked complete.

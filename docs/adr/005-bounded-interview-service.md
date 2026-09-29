# ADR 005: Limited single-instance interview service with persistent budgets

## Status

SUPERSEDED by ADR 006 before implementation. Historical proposal only: do not execute the separate-service, SQLite or invitation design below.

## Date

2026-09-28

## Context

Anonymous hosted model access needs enforceable admission/spending boundaries and deterministic recovery from crashes and ambiguous provider outcomes.

## Decision

Use a Go HTTP service with one active instance and durable SQLite, invitation-based pilot access, hashed/derived session tokens, versioned turns, strict request sizes and persistent idempotency. Reserve worst-case configured costs transactionally before model calls and retain uncertain reservations. Provider/model/pricing/caps are operator configuration; absent values fail closed. Default proposed session retention is 24 hours. CLI downloads no server key.

## Consequences

Horizontal scaling needs a later storage decision. The service remains useful locally against fixtures without spending. Hosted production must have TLS, durable volume, external provider cap, deletion/backup policy and authorized operator. Invitations reduce pilot abuse but are not a complete public sign-up system.

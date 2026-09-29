# ADR 006: Reuse the existing Sanifu chat interview backend

## Status

Accepted founder direction. Supersedes ADR 005 and the separate-service portions of ADR 001, architecture and initial task contracts before implementation.

## Date

2026-09-28

## Context

The initial plan independently proposed another Go interview service, storage engine, invitation/session protocol and deployment. The founder asked whether Anza reused chat and then explicitly requested revision to reuse the same backend/interview. Source inspection confirmed chat already has model routing, provider secrets, server-held conversation history, recovery tokens, DynamoDB conditional writes, brief approval/deletion and a deployed Lambda/HTTP API stack.

## Decision

Anza is the local Go CLI. Extend the existing chat server with immutable setup-purpose conversations and an additive setup policy, context and structured recommendation endpoints. Reuse /api/ask, /api/conversation, token hashing, transcriptStore/CAS/TTL, shared turn/model helpers and existing deployment. Preserve the default intake/contact/title/brief/booking behavior. Do not create cmd/anza-interview, another model router, SQLite store, invitation login or separate service host.

Use a setup-only coordination table in the existing stack for persistent admission/replay/cost reservations; existing per-instance limits alone are insufficient for this new bounded usage claim. Preserve existing shared provider billing/secrets and review incremental scope/caps before rollout. No budget increase is inferred.

Cross-repository task contracts name repository plus relative owned paths. The public CLI has no build/runtime dependency on the private chat module. Versioned JSON fixtures and a reviewed catalog metadata export define the seam; integration runs the real chat router under synthetic providers. Deploy additive backend capability before advertising a compatible CLI.

Do not harvest browser recovery tokens. Optional learner-selected approved brief import remains available, including plain-text briefs because existing LearnerBrief.Text is not structured project JSON. Reuse actual shared retention/provider disclosures; exclude setup conversations from scheduled owner mail by default and preserve legacy intake mail behavior.

## Consequences

This removes duplicate hosting/auth/storage/provider work and keeps one interview backend to operate. Changes touch a live shared backend, so isolated worktrees, peer reconciliation, feature gating, original website regressions and setup-only rollback are explicit requirements. Persistent setup quotas and structured recommendations are new functionality, not claimed to exist already. No backend source or deployment was changed by this planning revision.

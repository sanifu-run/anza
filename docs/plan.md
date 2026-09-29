# Anza implementation plan

Date: 2026 09 28. Revision: 2, shared chat reuse. Status: planned, not implemented. Execution target: GPT-6-Luna.

## Context

Anza prepares Sanifu participants to build their own real project. It is a Go CLI with a one-line OS-specific bootstrap, installs both Codex and Claude Code, supports macOS/Windows/Linux, and offers beginner/developer guidance. The existing Sanifu chat backend runs an LLM setup interview before participant agent login. Founder explicitly directed reuse; no independent interview server is planned. Participants use their own ChatGPT/Claude subscriptions or OpenRouter keys for development. Paying customers are optional. Content is a curated blend of portable founder workflows and ECC.

The product brief separates founder decisions from implementation defaults. No arbitrary model-generated command is executable. Native installation coverage is a qualified matrix; recommendations remain open to all project domains. Optional learner-selected brief import reuses facts without harvesting a website token. See ADR 006 and research/chat-reuse.md for the shared-backend boundary. See product-brief.md, design.md and contracts/v1.md.

This plan is deliberately fully decomposed at the founder's explicit request for exhaustive parallel contracts. This overrides the planning skill's usual outline-only treatment of later phases. Detailed later work is conditional, not permission to skip dependency review or evidence gates. No code implementation, paid model call, deployment or release has run.

## Discovery Summary

Anza is greenfield; chat already implements the shared HTTP/model/conversation/DynamoDB/deployment foundation. New setup capability is still PLANNED and untested. Reuse and remaining gaps are documented in research/chat-reuse.md. Do not describe the backend foundation as absent or rebuild it. The prior local setup/ECC inspection is summarized in research/source-audit.md. The public repository contains no approved product license yet. Kazi exists locally; all task rows carry acc predicates, but no Kazi proposal/goal is precomputed.

Content work is adaptation with provenance, not a copy of the founder's dotfiles. Operations work includes limited service admission, private credentials, durable budgets, retention and release qualification. Shared claim acquisition failed with a native Git identity error; planning artifacts were created sequentially in the empty checkout. Recheck claims before dispatch.

## Use Case Summary

17 planned use cases: 13 P0 and 4 P1. Canonical details and mappings: [usecases.md](usecases.md), [usecases.json](usecases.json). A local .claude/scratch/usecases-manifest.json copy supports the planning skill and is ignored by Git.

## Scope and Deliverables

| ID | Deliverable | Owner | Acceptance |
| --- | --- | --- | --- |
| D1 | CLI and preserving installer | Luna implementation pool | Real supported-platform installs and recovery preserve participant state |
| D2 | Setup mode in existing chat | Chat-scoped Luna workers / existing backend operator | Shared interview returns typed recommendations; intake regressions and persistent setup caps pass |
| D3 | Curated catalog and exercises | Luna content pool / reviewer | Every bundled asset has provenance; recipe limitations are explicit |
| D4 | Both agent access paths | Luna adapter pool / authorized tester | Four advertised subscription/OpenRouter paths have live evidence |
| D5 | Signed artifacts and bootstrap scripts | Luna release pool / operator | Clean download/install, integrity, update and rollback checks pass |
| D6 | Participant and operator handoff | Coordinator / founder | Real users reach accurate project readiness with documented manual actions |

Out of scope: completing customer projects, member/community apps, payments, automated deployment of participant software, shared development-agent proxy, wholesale ECC installation, mandatory third-party plugins/MCPs, model-written shell execution and unrestricted public free model access. SDK/store/cloud actions outside tested recipes remain manual. These exclusions do not restrict project topics in the interview.

## 5. Checkable Work Breakdown

Each epic links to exact owned outputs, dependencies, estimates, acc predicates and one task contract per row. All checkboxes begin open; generated counts describe plan state only.

### E1 -- Foundation and frozen contracts -> docs/plans/E1.md (0/5)

[Open E1](plans/E1.md).

### E2 -- Local platform and preservation primitives -> docs/plans/E2.md (0/6)

[Open E2](plans/E2.md).

### E3 -- Shared chat setup interview -> docs/plans/E3.md (0/8)

[Open E3](plans/E3.md).

### E4 -- Participant interview and review experience -> docs/plans/E4.md (0/4)

[Open E4](plans/E4.md).

### E5 -- Curated skills and project tool recipes -> docs/plans/E5.md (0/7)

[Open E5](plans/E5.md).

### E6 -- Deterministic planning and installation -> docs/plans/E6.md (0/4)

[Open E6](plans/E6.md).

### E7 -- Agent access and project readiness -> docs/plans/E7.md (0/3)

[Open E7](plans/E7.md).

### E8 -- Repair update removal and support -> docs/plans/E8.md (0/3)

[Open E8](plans/E8.md).

### E9 -- CLI integration bootstrap and artifacts -> docs/plans/E9.md (0/4)

[Open E9](plans/E9.md).

### E10 -- System and native qualification -> docs/plans/E10.md (0/4)

[Open E10](plans/E10.md).

### E11 -- Documentation authorization release and pilot -> docs/plans/E11.md (0/5)

[Open E11](plans/E11.md).

## 6. Parallel Work

One coordinator plus at most three concurrent workers in this harness. The pool target is GPT-6-Luna for agent work. Native/provider qualification may need an authorized operator. Human approvals occupy the coordinator, not a model worker. Do not exceed available runtime capacity or the shared build lease.

| Track | Tasks | Primary boundary |
| --- | --- | --- |
| Foundations | E1-E2 | Shared schemas first; separate package ownership |
| Shared backend | E3, T11.5 | Chat worktrees; reuse store/model/turn helpers; additive handlers and deployment config |
| Participant flow | E4 | HTTP client, brief import, wizard and review |
| Content | E5 | Disjoint catalog files; one final manifest owner |
| Installer/access | E6-E8 | Pure plan/adapters, journaled effects, evidence and lifecycle |
| Distribution/acceptance | E9-E11 | Native artifacts, real checks, scoped launch and pilot |

The table below is a conservative executable schedule: complete review/integration of dependencies before starting the next wave. Independent ready tasks may run earlier only after the coordinator regenerates the schedule and repeats path/dependency validation. Ownership is repository plus relative path. Do not concurrently dispatch two tasks that own the same qualified file, even in separate worktrees. Each worker stays in its declared repository; the coordinator handles cross-repo dependencies.

### Waves

| Wave | Concurrent worker slots | Tasks | Synchronization |
| --- | ---: | --- | --- |
| 1 | 1 | T1.1 | Review and integrate completed dependency outputs |
| 2 | 2 | T1.2, T1.5 | Review and integrate completed dependency outputs |
| 3 | 3 | T1.3, T1.4, T2.6 | Review and integrate completed dependency outputs |
| 4 | 3 | T5.1, T2.2, T2.1 | Review and integrate completed dependency outputs |
| 5 | 3 | T5.2, T5.4, T5.5 | Review and integrate completed dependency outputs |
| 6 | 3 | T5.6, T5.3, T2.3 | Review and integrate completed dependency outputs |
| 7 | 3 | T5.7, T2.4, T6.2 | Review and integrate completed dependency outputs |
| 8 | 3 | T6.3, T4.4, T2.5 | Review and integrate completed dependency outputs |
| 9 | 3 | T6.1, T3.1, T7.1 | Review and integrate completed dependency outputs |
| 10 | 3 | T6.4, T3.2, T3.3 | Review and integrate completed dependency outputs |
| 11 | 3 | T3.7, T4.1, T4.2 | Review and integrate completed dependency outputs |
| 12 | 3 | T7.2, T4.3, T7.3 | Review and integrate completed dependency outputs |
| 13 | 3 | T8.1, T8.2, T8.3 | Review and integrate completed dependency outputs |
| 14 | 3 | T3.4, T9.1, T3.8 | Review and integrate completed dependency outputs |
| 15 | 3 | T3.5, T9.2, T9.3 | Review and integrate completed dependency outputs |
| 16 | 3 | T3.6, T9.4, T11.1 | Review and integrate completed dependency outputs |
| 17 | 3 | T11.5, T10.1, T10.2 | Review and integrate completed dependency outputs |
| 18 | 1 | T11.2, T10.3 | Founder/coordinator gate present |
| 19 | 1 | T10.4 | Review and integrate completed dependency outputs |
| 20 | 1 | T11.3 | Review and integrate completed dependency outputs |
| 21 | 0 | T11.4 | Founder/coordinator gate present |

### Wave 1: Dispatch (1 workers)

- [x] T1.1 Create the Go CLI and dependency baseline

### Wave 2: Dispatch (2 workers)

- [ ] T1.2 Freeze shared schemas, fixtures and value types
- [ ] T1.5 Build task and catalog metadata checks

### Wave 3: Dispatch (3 workers)

- [ ] T1.3 Qualify vendor configuration and platform methods
- [ ] T1.4 Audit candidate skills and redistribution provenance
- [ ] T2.6 Plan preserving JSON and TOML configuration edits

### Wave 4: Dispatch (3 workers)

- [ ] T5.1 Load and validate versioned recipe and pack catalog
- [ ] T2.2 Implement private state, durable writes and locks
- [ ] T2.1 Inspect platforms and existing project tools read-only

### Wave 5: Dispatch (3 workers)

- [ ] T5.2 Author portable common teaching skills
- [ ] T5.4 Define web and automation toolchain packs
- [ ] T5.5 Define Python, data/ML and Go packs

### Wave 6: Dispatch (3 workers)

- [ ] T5.6 Define mobile, desktop and unsupported-domain paths
- [ ] T5.3 Define base agent and Git installation recipes
- [ ] T2.3 Implement bounded child process execution

### Wave 7: Dispatch (3 workers)

- [ ] T5.7 Seal and embed the complete reviewed content catalog
- [ ] T2.4 Verify downloads and extract archives safely
- [ ] T6.2 Implement Codex configuration adapter

### Wave 8: Dispatch (3 workers)

- [ ] T6.3 Implement Claude Code configuration adapter
- [ ] T4.4 Implement exact-plan review and digest approval
- [ ] T2.5 Implement OS credential storage and session-only fallback

### Wave 9: Dispatch (3 workers)

- [ ] T6.1 Build deterministic local installation plans
- [ ] T3.1 Extend existing conversations for setup purpose and state
- [ ] T7.1 Launch both agents with independent access choices

### Wave 10: Dispatch (3 workers)

- [ ] T6.4 Execute approved operations with receipts and crash recovery
- [ ] T3.2 Add persistent setup-only admission to the shared backend
- [ ] T3.3 Extract and reuse the existing model transport

### Wave 11: Dispatch (3 workers)

- [ ] T3.7 Reuse turn reservation with a setup-specific replay policy
- [ ] T4.1 Implement Anza client for the existing chat protocol
- [ ] T4.2 Import and review an optional learner brief

### Wave 12: Dispatch (3 workers)

- [ ] T7.2 Report local and paid readiness separately
- [ ] T4.3 Implement accessible interview wizard
- [ ] T7.3 Run a project-specific first verification exercise

### Wave 13: Dispatch (3 workers)

- [ ] T8.1 Implement catalog and binary update planning
- [ ] T8.2 Implement ownership-limited uninstall and repair
- [ ] T8.3 Create opt-in redacted diagnostics and local deletion

### Wave 14: Dispatch (3 workers)

- [ ] T3.4 Add a setup interview policy and typed recommendations
- [ ] T9.1 Wire the full CLI command graph
- [ ] T3.8 Protect shared admin, mail and deletion boundaries

### Wave 15: Dispatch (3 workers)

- [ ] T3.5 Implement additive setup handlers alongside existing chat routes
- [ ] T9.2 Implement POSIX one-line bootstrap
- [ ] T9.3 Implement native PowerShell bootstrap

### Wave 16: Dispatch (3 workers)

- [ ] T3.6 Wire setup mode into the existing server and preserve legacy routes
- [ ] T9.4 Prepare reproducible artifacts, integrity and CI
- [ ] T11.1 Write participant/operator guides and license packet

### Wave 17: Dispatch (3 workers)

- [ ] T11.5 Prepare additive setup configuration in the existing deployment
- [ ] T10.1 Test fresh, existing and interrupted complete journeys
- [ ] T10.2 Qualify real native platform installs and recovery

### Wave 18: Dispatch (1 workers)

- [ ] T11.2 Review shared-backend rollout and bounded live-check scope
- [ ] T10.3 Exercise installer and backend adversarial boundaries

### Wave 19: Dispatch (1 workers)

- [ ] T10.4 Qualify real agent authentication and model round trips

### Wave 20: Dispatch (1 workers)

- [ ] T11.3 Roll out shared setup mode and publish the qualified CLI

### Wave 21: Dispatch (0 workers)

- [ ] T11.4 Pilot with real participant projects and revise recipes

## Timeline and Milestones

Estimates are work-slice targets, not delivery commitments. Dependency, native runner, account and approval waits can dominate calendar time. Split a task before dispatch if its detailed implementation no longer fits 30-90 minutes.

| Milestone | Dependencies | Observable exit |
| --- | --- | --- |
| M1 Contract baseline | T1.1-T1.5 | Buildable CLI, frozen shared API fixtures, evidence-backed methods and rights inventory |
| M2 Isolated subsystems | T2.1-T2.6, T3.6, T4.3, T4.4, T5.7 | Shared chat setup routes and local plan inputs work against real local boundaries |
| M3 Complete local journey | T9.1, T9.4, T10.1 | CLI subprocess performs full fixture journey; artifacts are generated |
| M4 Qualified release candidate | T10.2, T10.3, T10.4, T11.1, T11.2 | Native/account evidence, security review and launch decisions recorded |
| M5 Published and observed | T11.3, T11.4 | Production interview/install works; pilot findings recorded |

## Risk Register

| Risk | Impact / likelihood | Mitigation / owner |
| --- | --- | --- |
| Agent config/version drift | High / high | T1.3 pins qualified versions; adapters fail on unknown syntax; release retests |
| Overbroad project support | High / high | Explicit recipe matrix and manual constraints; never fabricate readiness |
| Existing setup overwritten | High / medium | Preimages, typed edits, receipts, ownership and TOCTOU tests |
| Hosted endpoint abuse/spend | High / medium | Invitations, persistent reservations, external key cap, fail-closed admission |
| Model injection/invalid output | High / high | Strict recommendation schema; catalog-only execution |
| Windows/SDK prerequisites | High / high | Native tests, manual licensed SDK steps and no policy bypass |
| Credential/customer-data leakage | High / medium | Keychain/session-only secrets, minimized upload and allowlisted diagnostics |
| Supply-chain/integrity failures | High / medium | Pinned manifests/artifacts, HTTPS bootstrap trust, signed updater metadata, revocation |
| Native execution runtime remains unavailable | Medium / observed | One smoke only after configuration changes; no billing/policy bypass |
| Luna cannot complete ambiguous contract | Medium / medium | Stop, amend contract and retry boundedly; no silent model substitution |
| Incremental setup scope/budget | High / known | Reuse existing host/provider grants; gate additional resources/caps and public rollout on applicable scope |
| Shared website regression or email leakage | High / medium | Feature flag, purpose isolation, legacy fixture/browser regressions, setup email exclusion and backend-first rollout |

## Operating Procedure

Read [execution.md](execution.md). The coordinator is sole writer of Anza plan/status/shared interfaces; workers own only named repository outputs and evidence. Chat live work is peer-owned and must be reconciled before isolated worker checkouts. No root chat/brain plan is rewritten by this master plan. Use isolated worktrees once an initial local commit exists. Verify native model/runtime availability with a bounded read-only smoke after a changed condition; prior failures do not justify automatic repeat or bypass.

Acceptance levels are separate: planned, implemented with local evidence, native/provider qualified, released/live-verified, and pilot observed. Mark a coding task locally accepted only after its named tests and review; release claims additionally require T11.3. Contracts are not certified merely because their text passes structural checks. Certification requires a reviewed actual GPT-6-Luna execution.

Tests are planned for every implementation, API route and negative boundary. Anza terminal UI uses scripted terminal/subprocess checks. Because the shared backend serves a live website, preserve existing synthetic browser intake/title/brief/booking checks as release regressions even though no new browser UI is built. Format and vet every changed Go package; run configured linter and scoped race checks at integration. Heavy multi-package checks use the shared build lease and load gate. No paid model request occurs in ordinary tests.

## Progress Log

2026 09 28: Revised to 53 task contracts across Anza/chat, 11 epics and 17 use cases. Contract v1 revision 2 and ADR 006 replace the separate-service design; ADR 005 is superseded. No implementation or certification had been performed at that revision.

2026-09-29T01:24Z: Preflight found Anza on unborn main with 85 planning files, Chat clean at 26d79ab after contact-intake reconciliation, 6.8 GB free, one-minute load 11.25, and Go 1.27.1 darwin/arm64. No Anza or Chat worker was visible by command line; cwd inspection was unavailable because macOS could not resolve uid 501. GitHub task claim acquisition failed with `No user exists for uid 501`; work uses the documented serialized single-coordinator fallback. The planning baseline is local commit 0acc30b.

Wave 1: GPT-6-Luna completed T1.1 on isolated branch task/T1.1 as commit 1072410e29613a73936def25021d3402a648cef4. The coordinator reviewed and fast-forward integrated it, then reran `go test ./cmd/anza -count=1` (3 tests) and `go vet ./cmd/anza` with temporary HOME/cache and module lookup disabled; both passed. Red/green and disabled-behavior evidence is in [T1.1 evidence](evidence/T1.1.md). No Chat files, paid calls, or publication changed.

Before Wave 2, amended T1.5 acceptance to preserve the newly reviewed T1.1 certification while leaving unexecuted rows `not_run`; the validator must never rewrite plan execution status.

## Hand off Notes

T1.1 is locally implemented and reviewed. Resume with Wave 2 tasks T1.2 and T1.5 after the accepted T1.1 dependency is integrated. Initial plan and implementation commits are local; no remote publication is implied. Optional brief import and immutable setup mode respect the shared Sanifu intake boundary. All local dotfile inspection details stay in private discovery records; only sanitized design is in this public repository.

The launch gate reviews existing chat deployment/provider grants and asks only for missing incremental setup limits/resource/live-check/publication scopes. Reuse the existing origin and data policy; verify setup mail exclusion and Anza licensing. No new host or invitation system is proposed. Unknown caps must not be guessed by workers.

## Appendix

- [Product brief](product-brief.md)
- [Architecture](design.md)
- [Contract v1](contracts/v1.md)
- [Execution index](execution-index.json)
- [Source audit](research/source-audit.md)
- [Execution instructions](execution.md)
- [Roadmap](roadmap.md)

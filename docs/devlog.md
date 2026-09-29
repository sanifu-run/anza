# Anza development log

## 2026 09 28 - Planning baseline

Created product brief, architecture, shared contract, 17-use-case catalog, 11 epic files, 51 task contracts, 21-wave execution schedule, five ADRs and worker handbook. All implementation/certification states are open. Kazi availability was observed, so task rows carry acc predicates; no goals/proposals were generated.

The remote claim attempt failed before ownership was acquired because native Git could not resolve its user identity. The empty checkout and exclusive file creation allowed sequential local planning without overwriting peer work. No worker execution, implementation test, dependency installation, paid call, commit, push or deployment was performed. Structural document checks are recorded separately when complete.

Structural validation: shared parser recognizes 51/51 tasks and 21 waves; all dependencies, use-case mappings, local links and concurrent path ownership checks pass. Negative structural fixtures rejected duplicates, dependency ordering errors and concurrent output collisions. See evidence/plan-validation.md.

## 2026 09 28 - Founder-directed shared chat reuse

Revised the architecture and all affected contracts to reuse the existing chat interview, model transport, conversation store and Lambda stack. ADR 006 governs; ADR 005 remains explicitly superseded history. Anza owns the local CLI. Setup-specific context, typed recommendations, durable quota reservations and mail exclusion are additive chat work. Existing intake/contact/title/brief/booking behavior has regression gates. No separate server, SQLite database or invitation service is planned.

The revised plan has 53 contracts across 11 epics and 21 waves: 44 Anza tasks and nine chat tasks. Repository-qualified ownership permits disjoint parallel work, with dependencies serializing shared files. Ordinary public CLI tests require no private repository; a dedicated shared-router contract run is a release gate. Shared parser recognized all 53 tasks and waves; structural dependency, ownership, contract, use-case and link checks passed, including three deliberately invalid fixtures. Estimates total 76 worker-hours excluding human gates and review/rework. No implementation, target-model certification, application test, live model call or deployment was performed in this revision.

## 2026-09-29 - Wave 1 T1.1

Completed the ownership/runtime preflight and the first implementation wave. The 85-file planning baseline was reviewed for local identity material and committed locally as `0acc30b`. Task claim acquisition failed because GitHub SSH reports `No user exists for uid 501`; per execution.md, work proceeds under the single-coordinator serialized fallback, without cross-session collision protection. Chat remained clean at `26d79ab`; its contact-intake work was reconciled before dispatch.

GPT-6-Luna implemented T1.1 in an isolated Anza worktree and committed `1072410e29613a73936def25021d3402a648cef4`. The coordinator reviewed and integrated it, then reran the scoped three-test package suite and vet with temporary HOME/cache and offline module lookup. Both passed. Red/green, disabled-behavior and environment evidence is in [T1.1 evidence](evidence/T1.1.md). No Chat source, paid calls, deployment or publication changed. Next is Wave 2: T1.2 and T1.5.


## 2026-09-29 - Wave 2 foundations

GPT-6-Luna completed T1.2 and T1.5 in separate Anza worktrees. T1.2 materializes contract v1 revision 2 with strict Go decoders, canonical digests, six JSON schemas and valid/invalid fixtures. Review added explicit null-collection and cross-platform path rejection. Its scoped domain tests/vet pass offline. Current Chat has no Anza setup wrapper fixture; the contract now keeps source-backed parity at T3.6 TestAnzaContractFixture.

T1.5 adds a read-only plan/catalog validator. Its 13 unittest cases and default validation pass; compatibility/content modes report the missing T1.3/T1.4 artifacts as expected. Both tasks are locally accepted and recorded in the execution index. No Chat source, paid calls, deployment or publication changed.


## 2026-09-29 - Wave 3 local acceptance

GPT-6-Luna completed T1.3, T1.4 and T2.6 in isolated worktrees. Coordinator reran the compatibility/content validators, all 13 plan-checker unit tests, and the scoped config-editor test/vet; all passed. T1.3 remains documentary-only across 24 platform cells and was reconciled with current official install guidance. T1.4 excludes founder material pending rights permission and records original replacement tasks. T2.6 provides pure JSON/TOML byte planning and drift-aware inverse metadata, with unsupported TOML syntax refused conservatively. Branch commits: T1.3 `b0e08f50df68b065a944b30d62ee637faa915fc9`; T1.4 `9fd0142c59addb9809c2a75e5a2c011362cc6b85`; T2.6 `005dd3aad0b68d3106ac8eaed9df1eab3f18cfcf`. Merged locally to main; no Chat source, user config, paid call, deployment or publication changed. The next prescribed wave is T5.1, T2.2 and T2.1.


## 2026-09-29 - Wave 4 local acceptance

GPT-6-Luna completed T5.1, T2.2 and T2.1 in isolated worktrees. Coordinator reran scoped tests/vet for all three packages; all passed, as did `gofmt` and `git diff --check`. T2.2 additionally cross-compiled its Windows/amd64 tests; native Windows ACL/lock/durability behavior remains unqualified. T2.1 uses a consent callback before selected project-manifest reads; only synthetic fixture roots were used, and no real workspace/manifests/tools were inspected. T5.1 follows coordinator scope amendment `7a7b87c`: recipe/pack schemas validate, Exercise entries fail closed pending a schema, and the manifest remains empty until reviewed catalog assets exist. Branch commits: T5.1 `89c2fcd7aeb9caf9a4c173bf24c656a17fb8ad2e`; T2.2 `e8bd39f98cd7d803b13b3476d317a434bdc4b2b4`; T2.1 `1a8cb85b44d250e28c3d7a608d6e7b1dde312266`. Merged locally to main; no Chat source, user project, paid call, deployment or publication changed. Next is Wave 5: T5.2, T5.4 and T5.5.

## 2026-09-29 - Wave 5 local acceptance

GPT-6-Luna completed T5.2, T5.4 and T5.5 in isolated Anza worktrees. Coordinator reviewed and locally integrated their disjoint catalog content. T5.2 adds six original, tool-neutral skills with pack provenance; coordinator completed the required human review against Anza's real T2.6 config-edit feature as an internal project sample. T5.4 adds 14 Node.js v24.21.0 manual recipes with exact artifact metadata, web/automation packs, and no unqualified Playwright payload. T5.5 adds eight Python/Go manual recipes, packs, and provenance; GPU readiness stays manual/unmet because v1 has no typed GPU capability, and the named TestMLCapabilityConstraints is absent. T5.4/T5.5 source-backed static and synthetic fixture evidence is recorded per task. Integrated `go test ./internal/catalog -count=1` and `go vet ./internal/catalog` passed after shared load dropped below 10; artifact platform floors remain manual, and no participant project or actual runtime installation was exercised. Commits: T5.2 `784bb4d153eb4fce5e0f410960e116c21076cdeb`, T5.4 `b772f2b08ae0c70415b895ca72c216ecb1bd3290`, T5.5 `9d86b39b10d766119edbce4956a9e9b3d83a0c28`. No Chat source, installs, paid calls, deployment or publication changed. Next prescribed wave: T5.6, T5.3 and T2.3.

Wave 6 preflight: T5.3 and T2.3 are disjoint Anza tasks with their dependencies satisfied and are running in isolated worktrees. T5.6 is held before dispatch: its contract requires `catalog/exercises/mobile-desktop.json` and unsupported-project guidance, but Exercise descriptors are unversioned and T5.1 rejects them; the amended T5.7 contract also prohibits adding them until a schema/loader exists. The current Pack/Recipe interface has no typed arbitrary unsupported-domain response. Do not invent a descriptor or claim partial readiness; resolve the interface/contract gate before resuming T5.6. No Chat task is in this wave, so `../chat` remains untouched.

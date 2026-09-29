# GPT-6-Luna execution handbook

Status: active execution handbook. Local workers and builds are recorded in docs/devlog.md; paid API calls, releases and publication remain separately gated.

## 1. Coordinator preflight

1. Read applicable AGENTS.md, plan.md, design.md, contract v1 (current revision), ADR 006, execution-index.json, roadmap.md, and ajent.social where present. Each task has a repository field: anza or chat. Paths/commands are relative to that assigned worktree, never the coordinator cwd. Existing Sanifu vision/intake work is peer-owned. Reconcile the chat owner's uncommitted contact-intake changes before taking an isolated baseline; never overwrite its live main.go or privacy draft.
2. Inspect status/remotes, live process working directories, claims and current dependency artifacts. Preserve unrelated files. This public repository ignores ajent.social and local scratch state.
3. Create the initial local planning commit only after reviewing exact file selection; do not stage concurrent/unrelated work. An empty repository has no base commit for worktrees. A commit does not imply permission to publish. Establish one integration owner and an isolated worktree/branch per implementation task in its declared repository. Chat already has history; Anza needs its initial local planning commit. Never copy the private chat repository into the public Anza tree.
4. Use the shared claim skill for the task and shared plan resource. Check the literal WON response, retain its SHA and release only that claim by compare-and-swap. The planning attempt failed due native Git identity resolution; do not assume a lock exists. If remote claims remain unavailable, one coordinator may serialize shared writes and use disjoint task outputs as the documented headless-session fallback. Record this limitation; do not pretend it protects against unrelated remote workers.
5. Resolve GPT-6-Luna in the active runtime. The requested tool model identifier is `gpt-6-luna` where supported; validate actual availability rather than silently substitute. No paid API fallback or configuration/policy bypass. Native headless startup previously failed before thread start; retry once only after a changed condition, using a read-only smoke with a short timeout. If still blocked, report the evidence and leave dispatch pending.
6. This is an engineering task plan, not an instruction to dispatch marketing/product/revenue chiefs. If using the Sanifu organizational launcher, respect its direct-report contracts and model restrictions; do not assume it already implements this worker pool. A capable authorized runtime must supply isolated worktrees, the requested model and bounded execution. Do not invent a launcher command.
7. Start with T1.1. Once shared types exist, compare their actual signatures to contract v1. Any mismatch is repaired in the owning task before consumer fan-out.

## 2. Per-task dispatch packet

Give one worker exactly one task at a time:

```text
You are executing task <ID> in an isolated Anza worktree.
Read the assigned repository AGENTS.md and the coordinator-supplied Anza
master task docs/tasks/<ID>.md, contracts/v1.md (current revision), design.md,
execution.md and ADR 006. Chat worktrees do not contain the master plan;
receive the exact task/context packet rather than guessing sibling paths.
Target model: GPT-6-Luna. Do not spawn additional agents.
Implement only the owned outputs in the task plus its evidence file.
Upstream dependency artifacts: <coordinator fills current checked-out state>.
Task purpose and acceptance are in the contract; do not invent missing success.
If a shared interface or dependency needs changing, stop and propose the exact
amendment. Never edit another task's outputs or the shared plan to get green.
Use local fixtures for external systems. Live calls/installations require the
specific native/paid qualification task and its authorization.
Return: observed result; files changed; tests and counts; red/green evidence;
known limitations; proposed follow-up. Do not mark yourself certified.
```

Runtime facts such as current branch, open PR overlaps, lock SHA, installed tools and spend grants belong in the dispatch packet, not durable task contracts.

## 3. Parallel scheduling and ownership

Use the 21 explicit waves in plan.md. Maximum three workers plus coordinator in this harness; one worker per task. `kind: human` is founder/coordinator work. `kind: any` allows an agent to prepare/execute authorized checks, but missing native hardware or credentials remains blocked. Human gates may occur alongside unrelated implementation; they do not unlock dependent tasks until recorded complete.

Repository-qualified paths are the ownership unit. A shared filename in different repos is not a collision; two tasks editing chat/main.go are serialized by dependencies/waves. Never run dependents against production stubs. Consumer-defined interfaces and test fakes allow isolated unit work after the shared contract; final wiring still depends on real implementations. Test fakes must live in *_test.go or an explicit testutil package. No TODO success, hardcoded successful response or fake readiness in production paths.

Anza command wiring is foundation then full CLI integration. Chat main.go is narrowly owned in sequence by transport refactor, optional cleanup-hook addition and setup router/runtime wiring; preserve the peer's accepted prompt baseline. Root dependency changes are coordinator-owned through an amended contract. Catalog content files are disjoint; T5.7 alone seals the final manifest and emits sanitized setup metadata. T3.4 copies/pins that artifact into chat. Neither module imports the other; record actual paired catalog digests at integration. Config adapters are pure and complete before plan digest generation. The executor cannot add effects after participant approval.

Do not hold a plan-file claim across a worker run. Workers append sanitized per-task evidence; the coordinator updates plan/index/roadmap after review. Re-read current files before every write. No wake signals, polling daemon or assumptions that a file post was received.

## 4. Build and test discipline

Tasks list exact future commands. A semicolon-separated command list in execution-index.json represents sequential checks, not a license to ignore a failing command. Stop on the first failure and record it. Tests named in contracts are requirements to implement, not evidence of tests existing today.

Use meaningful tests of public behavior. API tests make real HTTP requests through the router and assert status/body. CLI tests launch the command and assert output, exit and filesystem effects. Platform tests run native artifacts in disposable accounts/VMs; cross-compilation and mocks do not qualify an OS. Anza has terminal UI, so browser automation is not a substitute for terminal/native tests. The reused backend serves the existing website: preserve its synthetic browser ask/title/brief/booking/contact regressions before shared-backend release.

Run gofmt on changed Go files, go vet on changed packages and the configured linter at integration. Before any multi-package go build/go test/golangci-lint (or other heavy build), check uptime and hold above one-minute load 10. Claim the shared build lease through the installed claim skill with CLAIM_REMOTE set to the shared build-lease repository named in the applicable machine AGENTS.md and resource R-build-lease. Inspect WON, save SHA, run the check in the same foreground process, release that SHA immediately using a trap. Do not run more than one broad race suite at once. Ordinary compilation/testing must not install software into the developer's real home. Chat forbids paid CI; use existing authorized/free or local capacity and leave absent native hardware unqualified.

Use synthetic credentials, localhost provider/download fixtures, temporary user roots and injected clocks. A fixture endpoint exception to HTTPS must be explicit test-only dependency injection, never a production flag that accepts arbitrary remote HTTP.

For behavior changes show a meaningful red observation, implementation green, and the corresponding negative test failing when its guard is deliberately disabled in the isolated task worktree. Restore only the worker's own temporary edits. For docs/metadata validation use a malformed copy in a temporary directory. Avoid destructive repository commands on shared work.

## 5. Acceptance and certification

- Planned: contract/schema/ownership exists; no product behavior claimed.
- Locally implemented: real source and named local tests pass, formatting/static checks pass, coordinator reviewed.
- Qualified: relevant native platform/provider checks observed and recorded.
- Released: scoped publication/deployment completed and production checks observed.
- Pilot observed: consenting users completed actual project readiness, limitations preserved.

Do not blur these levels. A coding task may be locally accepted without production publication; the product is not release-complete until T11.3. The planning skill's live-verification requirement is enforced by that explicit release dependency, not unauthorized per-task deployments.

For each task record actual command, executed test count, result, environment, synthetic/live evidence class and unresolved concerns. Mark `Certified: GPT-6-Luna <UTC date> <reviewed commit or PR>` only after that exact model actually completed the contract and the coordinator accepted it. All contracts currently say NOT RUN.

Two failed attempts with the same cause stop that task. Record missing interface/fixture/permission and propose a bounded contract amendment. Do not use an expensive model silently to mask an underspecified contract. Other disjoint tasks may continue. Review/rebase/integrate one result at a time; rerun affected interface checks after integration, then release its claim. Push/PR/merge only within applicable repository authority.

## 6. Gates and evidence

M1 freezes contract and compatibility evidence. M2 checks shared-chat/client catalog parity and legacy-route preservation. M3 exercises the full local journey. M4 requires real native/provider results and launch decisions. M5 checks actual published artifacts and live interview behavior.

T11.2 checks existing chat/provider authorizations and presents only missing incremental setup-resource/cap/live-check/publication scope. Reuse of the existing host/model is the founder decision; do not propose a duplicate backend. No silent increase to existing budgets. T10.4 live provider checks and T11.3 deployment/publication require applicable recorded scope; missing OS/accounts keep qualification open.

T11.5 prepares additive DynamoDB coordination/config/IAM changes in the existing chat Pulumi stack. It must not replace transcript storage, Lambda identity, API origin or provider-secret references. T11.3 deploys the shared image/config with setup disabled, checks legacy routes, enables compatible setup and publishes the CLI last. Rollback disables only the new setup surface first. No separate host, OCI service or SQLite volume is built.

## 7. Cross-repository execution seam

The master plan, contracts, index and use cases live in Anza. Nine task contracts are owned by chat; the rest by Anza. A chat worker receives a read-only copy of its master contract plus the current shared protocol and uses a chat worktree. Task evidence is written to its own repository and summarized without private records into Anza. The coordinator must not treat a linked task as authorization to edit an unrelated live workspace.

T10.1 uses ANZA_CHAT_SOURCE supplied by the coordinator to run the real chat TestAnzaContractFixture with synthetic credentials/storage/model responses. Both source revisions and catalog digests are recorded at execution time. Fixtures can validate the shared protocol without a Go module dependency or paid call. Anza CI remains runnable without private GitHub access; the paired integration gate runs in an authorized workspace. No public CI token grants private chat access.

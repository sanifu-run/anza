# Anza launch completion plan

Progress: 2026-10-01 — local qualification fixes merged and pushed; live,
native-platform, release and participant evidence remain gated.

This supplements the existing WBS without replacing its task contracts or
historical evidence. Governing architecture remains ADR 006: reuse the shared
Chat backend. Anza main is 7c76019; Chat main is 50c7a80. The accepted legacy
draft fix c619cb1 and release architecture fix 3a9b770 are already on remote
Anza main. Local CLI journeys passed against committed Chat 869a89d. These
results do not establish native platform support, live providers or deployment.

## Outcomes and scope

Finish the qualification needed for safe installation, isolated provider use,
budget enforcement, publication and a consented pilot. Existing use cases and
T10.2, T10.3, T10.4, T11.2–T11.5 remain the acceptance authority. Do not add
features, copy live credentials, access real project files during provider
qualification, or publish before review of a concrete rollout packet.

## Immediate agent frontier

| Lane | Deliverable and acceptance | Dependency | Estimate |
| --- | --- | --- | --- |
| Security | Refresh Chat dependency alerts; triage exploitability and repair required issues with regression evidence. The push warning was 19 alerts, not a current assessment. | Current remote source and advisory access | 60–90 min first pass |
| Platforms | Execute available native installation, credential-store and release checks under T10.2/T10.3. Record unavailable Windows/Linux environments explicitly; mocks never qualify a native cell. | Available native runners | 60–90 min first pass |
| Providers | Prepare an isolated adapter harness, protected local key prompt, readonly synthetic fixtures and attempt/spend accounting; qualify T10.4 access paths after human login/key entry. | H1, H2; verified hard caps | 60–90 min preparation |
| Runtime/cost | Verify actual gateway token/pricing semantics and reservation arithmetic; refresh additive infrastructure preview and reconcile transcript-mail scope with current Chat source. Deliver T11.2/T11.5 review packet. | Account access; provider evidence for live claims | 60–90 min first pass |

Dispatch at most three GPT-6-Luna workers in a wave, with one coordinator for
contracts, integration and final verification. Before dispatch, read project
AGENTS.md and ajent.social, claim ownership, and assign exact files in isolated
external-SSD worktrees. Chat source changes run only in Chat worktrees. Security
and runtime lanes may not share files; serialize amendments through the
coordinator. Follow shared build lease/load limits and keep caches on the SSD.

For every batch, review source and relevant tests, record actual evidence,
merge only qualified changes and push immediately. Leave unrelated failing
branches isolated. No worker self-certifies; unavailable evidence stays open.

## Human handoff in Blink

| Item | Human-only outcome | Readiness |
| --- | --- | --- |
| H1 | Sign into fresh isolated Codex and Claude subscription directories on this Mac. No new Mac account or copied credentials. | Ready |
| H2 | Enter the separate capped OpenRouter key into the prepared protected local adapter prompt. Never put it in chat, Blink or a plaintext file. | Wait for protected prompt |
| H3 | Identify signing-key custodian and intended release channel; agents implement the resulting signing workflow. | Ready |
| H4 | Review the exact deployment/publication packet and approve its concrete scope. | Wait for qualifications and preview |
| H5 | Identify consenting beginner/developer pilot cases; include supported OS coverage before broad support claims. | Ready |
| H6 | Observe the released pilot participants completing and resuming their project; report anonymized outcomes. | Wait for release and consent |

Blink holds execution runbooks and completion status. Private task IDs and local
authentication paths stay outside tracked documentation. Ready blocks may be
scheduled now; dependent reviews and pilot observations stay unscheduled until
their prerequisites exist. External calendar visibility is currently unavailable.

## Gates and later waves

1. Provider qualification: at most 12 request attempts and $1 total API spend,
   including failed attempts; isolated subscriptions and capped test key only.
   Verify caps again before calls. No real projects or production keys.
2. Rollout packet: native support matrix, security disposition, live provider
   evidence, current preview, exact resource/mail scope, routing, retention,
   rollback, artifact digests, signing custody and cost reservations. Approved
   ceilings are 10 admitted sessions/day, 12 calls/conversation and $1/day setup
   reservations within $100 Experiential and $50/month AWS budgets. Ceilings
   do not promise full daily capacity; repairs and existing conversations count.
   Apache licensing is approved. Existing grants cover routine engineering;
   deployment and publication require review of this concrete packet.
3. T11.3 release wave: after H4, build/sign/publish only the approved matrix,
   verify clean-client installation, update/rollback and production golden and
   negative checks. Stop if preview drift or budget semantics invalidate approval.
4. T11.4 pilot wave: after qualified release, observe consenting real cases.
   Record active setup time, missing recipes, help, readiness, first meaningful
   check, independent resume and next-step understanding. Keep failures and
   untested domains visible; commit only anonymized aggregates.

The coordinator updates existing task evidence and execution status at each
gate. Estimates bound the next work session, not a delivery promise. Unknown
runner access, account semantics, security findings or signing custody trigger
scoped follow-ups rather than unsupported success claims.

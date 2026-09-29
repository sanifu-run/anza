# Candidate content inventory

Audit date: 2026-09-29. Scope: selected founder plan/apply/verify/review/journal/Go workflows and selected ECC planning, verification, Go-domain, and installer-safety assets. Source checkouts were read-only. Exact source revisions, paths, rights findings, assumptions, prerequisites, and decisions are recorded in [content-sources.json](research/content-sources.json).

## Redistribution decision

No founder workflow text is cleared for redistribution. The inspected shared skills repository has no tracked `LICENSE` or `NOTICE`, and its checked-in skill files do not declare a redistribution license. Personal authorship or local availability does not establish permission. Exclude those source files unless written permission is recorded later.

ECC's inspected `LICENSE` is MIT at revision `d3b8a3e908904e242ed2dbe66af62cca71131419`. This is direct repository-level license evidence for the selected tracked files, with the MIT notice and attribution conditions preserved. It does not establish rights for untracked dependencies, bundled third-party material, external services, or every asset in ECC. Selected MIT material is therefore a source for limited adaptation of ideas; do not copy skill prose or code. `scripts/lib/install/hook-consent.js` explicitly credits a contributor for capability-disclosure and held-materialization semantics; preserve that attribution if any substantial contribution is adapted. This inventory itself contains no copied skill passages or code.

The Anza repository has no approved product license yet. That question remains for its release/license task and is separate from whether these upstream sources may be used. Public candidate exports must contain no personal paths, private infrastructure, customer data, credentials, or unreleased content.

## Recommended original portable skills

Write these as new Anza-authored material after the content task is dispatched. Keep instructions tool-neutral and express harness actions as small adapters or examples rather than required syntax.

| Proposed asset | Portable intent | Source decision and rights | Prerequisites for Anza version |
| --- | --- | --- | --- |
| `anza-scope` | Clarify outcome, audience, constraints, and safe boundaries before changing a project; preserve existing work and ask only when a choice has no safe default. | Original replacement for excluded founder planning concepts; no source prose included. | Project files and user request only. |
| `anza-plan` | Turn an agreed outcome into dependency-ordered, reviewable work with owners, acceptance evidence, and explicit gates. | Original replacement; ECC planning workflow is MIT-licensed inspiration only. | Repository files; optional Git status. No specific agent-team API. |
| `anza-build` | Make one bounded change, preserve neighboring work, and keep effects within the approved plan. | Original replacement; concepts from the founder apply workflow remain excluded; ECC domain guidance may inform a fresh version. | The project's actual language toolchain and task contract; no Anza-wide script assumed. |
| `anza-check` | Choose checks from observed project configuration, record commands/results, and distinguish local evidence from native, provider, or production qualification. | Original replacement; verification-loop material is MIT-licensed inspiration only. | Only tools configured by the project. Provider or deployment actions need separate authorization. |
| `anza-review` | Inspect a bounded diff for correctness, preservation, security, and public-content leakage; report findings with file evidence. | Original replacement; founder review workflow is excluded; ECC review concepts may inform fresh wording. | Git diff where available; no mandatory subagent names or plugin command. |
| `anza-continue` | Leave a concise handoff with state, decisions, evidence, limitations, and the next dependency. | Original replacement for excluded founder journaling workflow. | Task evidence destination selected by Anza; no private journal path. |

Keep optional domain recipes in a separate catalog, not inside the portable core. The inspected ECC Go testing material is MIT-licensed at the recorded revision and can inform an original Go recipe. Its examples assume Go tests and should not become prerequisites for non-Go projects. Do not bundle unrelated ECC skills or an ECC installer/runtime.

## Harness, tools, and scripts

Founder skills use harness-specific commands, deferred tools, mutable home-directory conventions, and local scripts. In particular, planning refers to `plan/DISCOVERY.md`, `plan/STRUCTURE.md`, and synchronization scripts; apply dispatches through phase/rule/pool references and optional `kazi`; verify assumes report/scratch conventions and named browser tools; review asks for a harness question tool; journal writes to a project devlog; Go guidance invokes `gofmt`, `go test`, and optional `golangci-lint`. These are documented in the JSON per row and support-file lists. None should be copied as an unconditional Anza prerequisite.

ECC planning references its own agent catalog, orchestrator command, plugin-versus-legacy install detection, and harness home layout. ECC verification-loop examples assume JavaScript/Python command names and `grep`; the Go recipe assumes the Go toolchain. The selected installer files depend on other ECC modules and Node code. Anza should implement its own owned-file and consent boundaries in its Go installer; importing ECC Node scripts is out of scope.

## Deferred rights and content work

- Obtain written permission or keep the six founder source files excluded. Permission must identify the files, allowed modification/redistribution, attribution, and commercial/public use.
- For any future ECC reuse, inspect the exact file and all included examples/assets at the pinned revision, preserve MIT notices where required, and resolve any file-specific contribution or third-party notices before bundling.
- Author the six portable skills from the intents above and review them against Anza's approved product license, beginner/developer audience, platform support, and no-arbitrary-command execution boundary.
- Domain content beyond the limited Go testing recipe remains deferred until each candidate has its own source and rights review.

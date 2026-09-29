# Use case catalog

All 17 use cases are PLANNED; no implementation or test evidence exists yet. Canonical machine-readable source: usecases.json. The local scratch manifest mirrors it for planning tools.

| ID | Priority | Participant/operator outcome | Entry |
| --- | --- | --- | --- |
| UC-001 | P0 | Install and launch Anza with one OS-appropriate command | `anza / bootstrap` |
| UC-002 | P0 | Inspect and review the local project environment | `anza inspect --json` |
| UC-003 | P0 | Describe any real project through a hosted interview | `anza setup / chat setup start + /api/ask` |
| UC-004 | P0 | Receive a tailored and bounded setup recommendation | `anza interview / chat setup recommendation` |
| UC-005 | P0 | Review and approve the exact installation changes | `anza plan / anza apply` |
| UC-006 | P0 | Install prerequisites and both agents | `anza apply` |
| UC-007 | P0 | Use curated portable skills in both agents | `project skill discovery` |
| UC-008 | P0 | Choose subscription or OpenRouter independently per agent | `anza run codex|claude` |
| UC-009 | P0 | Resume, repair or reconcile interrupted setup | `anza setup / anza repair` |
| UC-010 | P0 | Check local readiness and optionally verify live access | `anza doctor [--live]` |
| UC-011 | P0 | Complete a first check relevant to the participant project | `setup exercise stage` |
| UC-012 | P1 | Review and apply a safe update | `anza update` |
| UC-013 | P1 | Remove Anza-owned changes safely | `anza uninstall` |
| UC-014 | P0 | Verify release provenance and recover from bad releases | `bootstrap / updater` |
| UC-015 | P1 | Export sanitized diagnostics on request | `anza diagnostics` |
| UC-016 | P0 | Operate bounded setup mode in the shared chat backend | `chat setup mode / DELETE /api/conversation` |
| UC-017 | P1 | Import an optional approved project brief | `anza setup --brief FILE` |

# Shared chat reuse evidence

Inspected 2026 09 28. This is source evidence, not a fresh production qualification.

Initial source inspection used 4e0836f with concurrent contact-intake edits. At final planning review, the peer had committed its deployment record at 26d79ab and the checkout was clean; ajent.social reports the authorized contact-intake deployment. This planning session did not modify or deploy chat source. Future workers must reconcile the latest peer baseline and use their own worktrees.

| Existing source | Observed capability | Planned use / gap |
| --- | --- | --- |
| main.go server/newServerForModel | Shared HTTP client, selected key/model/endpoint, allowed origins and active semaphore | Reuse directly; setup is a server extension |
| main.go answerWithPrompt | One model exchange used by answer/title/brief, then citation processing; 45-second client deadline | Extract raw response once for typed recommendations while preserving wrappers |
| runtime_secrets.go | Existing provider/owner Secrets Manager loading | Reuse; no CLI/server-key sharing |
| transcripts.go conversationID | X-Conversation-Token: 64 lowercase hex, SHA256-derived storage ID | CLI creates its own compatible recovery token |
| transcripts.go Conversation/transcriptStore | Server-held turns, version/generation, Get/Put/List/Delete | Add optional immutable purpose/setup state without replacing session store |
| transcripts.go reserveTurn/finishTurn | Request ID/body checks, conditional leases, completed replay and brief invalidation | Share mechanics with setup policy; ambiguous paid retries need stronger setup-specific treatment |
| dynamodb.go | Conditional version/generation writes, consistent reads, expiry filtering and deletion | Reuse transcript table/TTL; use separate coordination rows/table for non-transcript quota data |
| brief.go LearnerBrief | Editable plain-text brief with exact-version explicit approval | Optional reviewed text import; do not pretend it is typed project JSON |
| main.go allow/active | Per-instance hourly/per-IP request limits and concurrency bound | Retain; add persistent setup-only limits without overstating legacy global control |
| infra/lambda | Existing Lambda/Web Adapter, HTTP API default route, Secrets Manager, transcript DynamoDB and SES mail | Add setup coordination/config to existing stack; no new interview host |
| infra/lambda/transcript_mail.py | Scans transcript records and sends owner copies | Explicitly skip setup-purpose conversations; legacy records still send under existing policy |

Current source/operating record describes 30-day AWS retention from last update, provider capture outside that limit and independent email copies. Setup privacy must use actual shared policy and explicitly identify its mail exclusion. Do not promise the discarded 24-hour separate-service policy.

Reuse is an implementation change to existing code, not just forwarding requests to an unchanged /api/ask. Setup prompt selection, structured recommendation schema, catalog compatibility, durable setup quotas and mode-isolation regressions must be built and qualified. No paid model calls, bookings, tests of implementation or deployment were performed during this inspection.

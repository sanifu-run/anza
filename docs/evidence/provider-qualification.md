# T10.4 provider qualification

Status: procedure prepared; **qualification NOT RUN; no provider cell is qualified**. No model request or tool-call request was made during this preparation.

## Owner authorization and readiness — 2026-09-30

The owner approved a bounded qualification ceiling of at most 12 model requests
and $1 total across this work, using only operator-owned disposable test accounts
with verified hard-capped credentials and read-only synthetic files. This is a
qualification-only ceiling, separate from the hosted interview pilot budget. It
does not authorize use of everyday accounts or credentials, or requests before
all prerequisites are evidenced.

A read-only local readiness check found credential/config sources present, but
did not inspect or print secret values or profile names. It did not establish
that any account is disposable, operator-owned for this test, or hard capped.
Runtime budget enforcement was not verified. Experiential is a separately
hosted interview provider; local credential presence does not establish its
budget or enforcement. No provider request was made. Therefore all positive,
negative, and mixed-provider cells remain **NOT RUN**. Do not use the available
local credentials for qualification; proceed only after the disposable account,
per-request and aggregate hard caps, and runtime enforcement are independently
verified and recorded.

## Local observations — 2026-09-30

The available executables report `codex-cli 0.159.3` and Claude Code `2.1.286`.
Their local help exposes Codex `exec`, `--model`, `--sandbox read-only`, and
configuration overrides; Claude Code exposes print mode, model selection,
allowed-tool selection, and JSON output. Version/help output is installation
evidence only, not authentication, entitlement, model, or tool-call evidence.
The runtime cannot bind loopback sockets and DNS resolution failed for both
the website and the recorded shared API. Native/provider qualification must
use an authorized test environment with functioning network access.

## Four independent access cells

| Agent | Access | Required credential source | Required result |
|---|---|---|---|
| Codex | Participant ChatGPT subscription | Dedicated operator-owned test account, explicit subscription login | Correct selected provider/model, harmless model response, real read-only tool call |
| Claude Code | Participant Claude subscription | Dedicated operator-owned test account, explicit subscription login | Correct selected provider/model, harmless model response, real read-only tool call |
| Codex | OpenRouter | Dedicated hard-capped test key, pinned model and Responses-compatible provider config | Correct selected OpenRouter model, harmless model response, real read-only tool call |
| Claude Code | OpenRouter | Dedicated hard-capped test key and Anthropic-compatible route | Correct selected OpenRouter model, harmless model response, real read-only tool call |

These are participant development-agent access paths. The hosted Sanifu setup
interview separately uses the recorded Experiential `gpt-6-luna` route; its
credential, budget, and data capture must not be confused with these four cells.

## Preconditions and bounded smoke

1. Record T11.2 approval for the exact accounts/keys, models, per-request and
   total smoke budget, maximum request count, operator, and time window. Verify
   dedicated key caps and available budget before any request. Subscription
   usage and API charges are recorded separately. No silent route or model
   fallback is permitted.
2. Use a disposable VM or separate OS test account. Do not reuse, log out, revoke,
   overwrite, export, or copy the operator's everyday agent credentials or
   global configuration. Preserve before/after digests of owned test config and
   unrelated test-account files. Keep tokens in native credential storage or a
   protected process environment according to the selected adapter; never in
   Git, prompt text, command arguments, transcripts, or the evidence file.
3. Pin the exact binary version and selected model. Verify its installed help
   and reviewed provider configuration before using an example command. Clear
   conflicting credential sources only within the disposable account. Record
   selected provider/model from observed runtime output without credential data.
4. In a disposable project create `qualification.txt` containing only
   `ANZA_SYNTHETIC_READ_CANARY`. First request: return the exact text `ANZA_OK`
   with no tool use. Second request: read that file using the permitted read
   tool, return the canary, and make no changes. Codex must run with read-only
   sandboxing; Claude must allow only the required read tool. No shell/network,
   package installation, user project, customer data, or deployment is needed.
5. Observe successful model output and an actual tool event, inspect the file
   digests afterward, and stop if output requests credentials, selects another
   provider/model, exceeds the budget, or reports an entitlement error. A
   successful login status alone does not qualify either result. Record actual
   usage/cost from the provider's authoritative usage view; preserve uncertainty
   when billing is delayed.

## Negative and mixed-provider cells

- Missing credential: use a fresh unauthenticated disposable account; expect an
  actionable authentication state and no success/readiness claim.
- Expired subscription/session: operator supplies an expired test account;
  expect an entitlement/authentication failure classified separately from
  installation failure.
- Revoked OpenRouter key: revoke only the dedicated test key under the recorded
  scope; expect rejected credentials and no alternate-provider fallback. Never
  revoke a shared production key.
- Account conflict: put competing credential sources in the disposable account;
  require explicit source selection or an actionable conflict, never silent
  subscription/API switching. Restore only the test-owned config afterward.
- Mixed mode: verify Codex subscription + Claude/OpenRouter, then
  Codex/OpenRouter + Claude subscription. Record both independent readiness
  results and demonstrate that a failure in one agent does not authenticate or
  downgrade the other.
- Credential lifetime: after the test runner exits, inspect the test-owned
  process/config artifacts for the credential canary. Remove test credentials
  through their own supported store interface and verify removal without
  exposing values.

## Provider configuration references

OpenRouter documents an Anthropic-compatible route for Claude Code and warns
about cached subscription login conflicting with its authentication token.
Its documented base is `https://openrouter.ai/api`; this differs from its
Responses API route at `https://openrouter.ai/api/v1/responses`. Preserve the
separation and validate the currently installed agent's provider contract.
Sources: [OpenRouter Claude Code integration](https://openrouter.ai/docs/guides/coding-agents/claude-code-integration),
[OpenRouter Responses endpoint](https://openrouter.ai/docs/api/api-reference/responses/create-responses).
Endpoint availability does not itself prove a Codex model/tool-call path works.

## Evidence to record per cell

Date, platform, exact agent version, provider/model, approved account class
(no identity or credential), authorization record, prompt/tool fixture IDs,
request count, observed model output verdict, observed tool-event verdict,
usage/cost and remaining cap, preservation verdict, credential removal verdict,
and any failure category. Keep raw private output outside the repository;
include only sanitized results here. All four positive cells, all negative
cells, and both mixed-provider cells currently remain **NOT RUN**.

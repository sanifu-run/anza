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

## 2026-09-30 account clarification and network preflight

Owner selected existing Codex/Claude subscriptions plus a separate capped OpenRouter test key. No additional macOS user account is required for isolated agent configuration directories. Do not copy existing credentials; separate logins and verified key caps remain prerequisites. `composio link openrouter` failed before connecting (`getaddrinfo ENOTFOUND backend.composio.dev`). Read-only `curl --head --max-time 10` checks for `auth.openai.com`, `claude.ai`, and `openrouter.ai` each exited 6 (DNS resolution denied/unavailable in this runtime). No credential was read or transmitted, no account was connected, and no inference ran. The approved 12-request/$1 total qualification scope remains unused; all four live cells remain NOT RUN.

Official Codex authentication reference reviewed: https://learn.chatgpt.com/docs/auth and configuration locations: https://learn.chatgpt.com/docs/config-file/config-advanced. The authentication documentation provides device login where localhost callbacks are unavailable; network access is still required.


## 2026-10-01 authorized-scope and isolated-login update

The operator authorized use of existing Codex/Claude subscriptions and a separate
capped OpenRouter test key, with a hard ceiling of 12 model-request attempts and
$1 aggregate spend for this qualification. This is a bounded authorization, not
account, entitlement, key-cap, or spend verification. The test remains limited to
synthetic read-only files. No credentials, keychain entries, auth directories, or
credential values were read, copied, printed, or transmitted by the CLI checks.
No model request was made, and the request/spend counters remain 0/12 and $0.

Read-only environment findings on 2026-10-01:

- `codex` and `claude` resolve to installed executables. `codex --help`,
  `codex login --help`, `claude --help`, and `claude auth --help` report the
  installed CLI surfaces; help does not establish authentication or entitlement.
- DNS resolution now succeeds for `auth.openai.com`, `chatgpt.com`, `claude.ai`,
  `openrouter.ai`, and `backend.composio.dev`. This supersedes the 2026-09-30
  DNS failure observation; it proves name resolution only, not HTTPS reachability,
  login, provider access, or API readiness.
- Composio search found no active OpenRouter connection. With the operator's
  authorization, a private connection named `T10.4 capped test key` was initiated
  for read-only account readiness. It is pending user authentication. No OpenRouter
  account/key metadata or credit balance is available yet, and no OpenRouter tool
  has been called. After the user completes the auth UI, list and confirm the
  connection is Active before reading only credit totals and current-key cap,
  remaining-limit, and usage metadata. Never include key material or hashes in
  this record.

### Supported isolated login setup

Codex supports ChatGPT device-code login (`codex login --device-auth`). Its docs
require device-code login to be enabled in account security settings or by the
workspace admin. A task-scoped `CODEX_HOME` is the documented isolation mechanism;
set it only as an inline environment variable for a child CLI process and place
its fresh directory on `/Volumes/BuildOffload`. Use a distinct fresh directory
for each access cell, with mode 0700, and configure
`cli_auth_credentials_store = "file"` so auth is written only inside that fresh
profile. Do not inherit existing Codex profiles or credentials. Use an explicit
ChatGPT login for the subscription cell. This approach reads no existing
credential file or keychain. The OpenAI authentication docs describe device login,
credential storage and status checking: https://learn.chatgpt.com/docs/auth.

Claude Code's documented `CLAUDE_CONFIG_DIR` override creates separate settings,
sessions, and OAuth credentials; macOS Keychain entries are also keyed to that
configuration directory. Use a fresh, permission-restricted config directory on
the external SSD for each Claude cell and run the first-login flow there. The
`--bare` flag is unsuitable for subscription qualification because it disables
OAuth/keychain reads and permits only API-key or apiKeyHelper authentication.
Claude's auth and configuration directory behavior are documented at
https://code.claude.com/docs/en/iam and the OpenRouter gateway binding at
https://openrouter.ai/docs/guides/coding-agents/claude-code-integration.

For Codex/OpenRouter, use a separate fresh task-scoped Codex profile and the
OpenRouter custom model-provider configuration pinned to the Responses wire API
and a full model slug. Supply the dedicated test key only through a hidden
terminal prompt or a protected child-process environment; never place it in
configuration, arguments, shell history, logs, or evidence. For Claude/OpenRouter,
use a fresh `CLAUDE_CONFIG_DIR`, `ANTHROPIC_BASE_URL=https://openrouter.ai/api`,
`ANTHROPIC_AUTH_TOKEN` sourced from the dedicated key, and explicitly set
`ANTHROPIC_API_KEY` to an empty string in the child environment. Confirm provider
and model from runtime status/output; login status alone is insufficient. OpenRouter
publishes the Codex configuration and per-key `limit`/`limit_reset` fields at
https://openrouter.ai/blog/tutorials/codex-cli-openrouter/ and
https://openrouter.ai/docs/api/api-reference/api-keys/create-a-new-api-key.

The subscription login and key-injection steps require the operator to complete
browser/device-code login and, for OpenRouter agent calls, provide the already
approved dedicated test key directly to the isolated local child process. Do not
ask for or accept the key in chat. Authentication setup should happen while the
operator is present; no login process was started during this readiness pass.

### Gates and exact bounded run shape

The $1/$12 grant does not by itself prove a hard cap. The OpenRouter key must be
shown to have an enforced non-resetting limit at or below $1 and sufficient
remaining balance before a paid request. OpenAI API monthly project limits can be
configured as hard limits or notification-only, but the ChatGPT subscription
route is not API-key billing; verify the selected subscription route and ensure
no extra paid API credential/fallback is present. Do not alter account/org
settings. See OpenAI's spend-control documentation:
https://help.openai.com/en/articles/9186755-managing-projects-in-the-api-platform.
The total request counter must be kept manually across all four cells; count
failed/rejected attempts too. Stop on model/provider fallback, ambiguous route,
uncertain billing, unexpected tool access, or any request/cost cap.

After the pending OpenRouter connection is active and key cap/balance verified,
reconcile the separate T11.2 scope approval before any inference; the T10.4 task
contract says no paid request before that scope is recorded. The 12-request ceiling
is tight: each of the four positive cells needs both a plain-response check and a
real read-tool round trip, and the latter may take more than one provider request.
Run the four cells across two mixed configurations (Codex subscription + Claude/
OpenRouter, then Codex/OpenRouter + Claude subscription), so the same positive
evidence also exercises each mixed mode. Budget up to two provider requests per
positive cell (8 total) and one expected-rejection attempt for each of missing
credentials, expired subscription, conflicting credential sources, and revoked
test key (4 total). Count failed/rejected calls too. This is only a ceiling plan:
Codex/Claude CLI flags do not provide a verified global 12-request limiter. Execute
one cell at a time, count observed provider requests before continuing, disable
fallback/retries, and stop if a tool round trip exceeds its two-request allowance
or the running total would exceed 12. If request count cannot be observed and
stopped reliably, do not start live qualification; leave affected cells NOT RUN
and ask the coordinator to narrow the recorded authorization or provide a suitable
bounded harness. Any negative attempt that cannot be arranged without disturbing
live credentials remains NOT RUN. The expected rejection cases must not trigger
fallback. No mixed-config or negative attempts were made in this pass.

Current qualification verdict: **NOT RUN** for all four positive cells, all
mixed-provider cells, and every negative cell. Verified so far: documentation,
CLI presence/help, DNS, and pending OpenRouter connection creation only. Still
required: user completes OpenRouter Composio authentication; read-only confirmation
of dedicated key cap and balance; operator browser/device-code logins into fresh
isolated profiles; safe local injection of the same dedicated capped key for
OpenRouter CLI cells; and coordinator records T11.2 live-check scope. No account
cell has been inferred green from authorization or connectivity.

# Shared chat launch authorization

Status: **owner-approved pilot limits recorded; deployment and enablement remain unauthorized pending a reviewed preview and verified cost reservations**.

This packet distinguishes approved pilot settings from cloud deployment,
provider qualification, publication, and participant-data approval. Unknown
values remain unset; this record does not infer them from prior budgets.

## Verified candidate and local changes

- Reuse the Sanifu Chat backend; do not create a new host or invitation
  service. The Anza plan identifies the existing Chat origin as the target.
- Chat commit `1502f4c` (merged locally to Chat `main`) adds a protected
  DynamoDB setup-coordination table with TTL, a table-scoped transaction
  policy, and default-off setup configuration on the existing Lambda source.
  Source review found no edits to the existing transcript table, Lambda
  identity, HTTP API resource, or provider-secret references. Anza commit
  `6c9116c` adds durable, idempotent daily session admission. These are local
  source changes, not evidence of a cloud preview or deployment.
- Setup configuration includes the table name, catalog version/digest, daily
  admitted-session cap, paid-call count cap per conversation, worst-case input,
  output and reasoning cost reservations, aggregate daily cost cap, and
  tombstone retention. Setup remains disabled by default. Each required cap
  must be a positive reviewed integer before setup can be enabled; the three
  per-call reservations must fit under the daily cap.
- Candidate retention inherits Chat's configured transcript retention. The
  source documentation says 30 days in its reviewed production configuration,
  but the active account/stack and deployed value have not been verified here.
  DynamoDB TTL is asynchronous; the API rejects expired rows at the boundary,
  while physical deletion may take additional days.
- Setup-only rollback is to disable the new setup surface. Existing website
  intake and email behavior must remain available. Chat documentation says
  owner email copies are excluded from setup; verify this in the proposed
  image/config before enabling.
- A read-only `pulumi stack ls --json` attempt from Chat's Lambda stack
  directory could not open `/Users/dndungu/.pulumi/credentials.json`
  (`operation not permitted`). No stack names, account/region, active config,
  or cloud state were retrieved. The Pulumi CLI is installed (v3.226.0), but
  this runtime cannot access its configured credentials.

## Authorization ledger

| Scope | Verified authorization | Remaining decision/evidence |
|---|---|---|
| Local implementation | The user authorized execution of the Anza plan, including the recommended no-cost durable admission design and signed-updater/schema-lock code. | None for these local source changes. This does not authorize deployment or recurring spend. |
| Existing backend target | Reuse the existing Chat service is the plan/ADR direction. | Chat’s dated deployment record names project `sanifu-chat-lambda`, stack `prod`, region `us-west-2`, and origin `https://vg78ulztb1.execute-api.us-west-2.amazonaws.com`. The unused ECS scaffold is not the target. Reconfirm the active account/state before applying; this runtime has not inspected authenticated state. |
| Existing provider route | The selected hosted Chat route is Experiential `gpt-6-luna`, using its existing secret reference. OpenRouter remains a participant-owned development-agent access path, not the selected hosted-interview provider. | The dated Chat deployment record records a founder-managed $100 Experiential budget and $50/month AWS authorization. Confirm current provider hard-cap enforcement and remaining budget without exposing secrets; setup must fit within these totals unless a separate increase is approved. |
| Additional cloud resources | Local source prepares one setup-coordination table, TTL, narrowly scoped permissions, and additive Lambda configuration. | Existing Chat hosting has a recorded $50/month AWS authorization; that does not qualify or price these setup additions. No setup-specific Pulumi preview/apply evidence was supplied. No deletion/replacement safety or incremental monthly cost has been observed. |
| Setup pilot limits | Owner approved a pilot of at most 10 sessions per day, 12 calls per conversation, and $1/day in hosted model spend, all within the existing $100 Experiential and $50/month AWS authorizations. | Candidate Pulumi settings are listed below. Input, output, and reasoning reservation values remain unset until verified pricing is recorded. These limits do not increase either existing budget. |
| Preview and deployment | Prepare only a reviewed Pulumi preview before any deployment. | A preview has not been produced because this runtime cannot access the configured Pulumi credentials. Deployment and setup enablement remain unauthorized. Review exact resource changes and verified costs before requesting any later deployment authorization. |
| Provider qualification | Owner approved up to 12 model requests and $1 total for T10.4 qualification, only with operator-owned disposable test accounts and independently verified hard-capped credentials; use read-only synthetic files. | Account ownership, hard caps, and runtime budget enforcement are not established in this environment. Qualification is **NOT RUN**; do not make requests until all conditions are verified. |
| Retention, email, and participant data | Candidate inherits existing Chat settings and intends to exclude setup from owner email. | Verify actual retention and backup/PITR settings, physical-expiry limits, email exclusion, participant notice/deletion path, and data policy against the selected stack. |

| Anza license and CLI publication | Owner approved Apache License on 2026-09-30; Apache License 2.0 now covers original Anza code/documentation. Third-party licenses remain intact. No CLI publication scope was recorded. | Select/record the license, release channel, package/artifact scope, signing-key custodian, and publication authorization. T9.4 provides code plumbing only; no production signing key or secret is configured. |


## Approved pilot settings — 2026-09-30

The owner approved these candidate Pulumi values for the pilot, with setup
remaining disabled:

```text
anzaSetupEnabled=false
anzaSetupDailySessionCap=10
anzaSetupPerConversationCallCap=12
anzaSetupDailyCapMicroUsd=1000000
```

Input, output, and reasoning cost reservations remain unset pending verified
current pricing. Do not invent or estimate them. The approved $1 daily hosted
pilot ceiling is within the already authorized $100 Experiential budget and
$50/month AWS budget; it does not authorize either budget to increase. Prepare
only a reviewed preview before any deployment. This approval does not authorize
`pulumi up`, setup enablement, or a live provider call.

The separate T10.4 provider qualification approval is capped at 12 model
requests and $1 total across qualification work, restricted to operator-owned
disposable accounts with verified hard caps and read-only synthetic files. No
provider request is authorized until those conditions and runtime budget
enforcement are evidenced.

## Proposed rollout sequence

1. Resolve the actual existing Chat target and obtain a read-only authenticated
   Pulumi preview. Review every resource; require no deletion or replacement
   of existing transcript, Lambda, HTTP API, secret, and mail resources, no
   secret output, and only the expected additive table/policy/config changes.
2. Resolve verified current input, output, and reasoning prices and set reviewed
   positive reservation values that fit the approved $1/day cap. Keep setup
   disabled and confirm the pinned catalog and immutable candidate image.
3. Prepare and review the Pulumi preview, including resource replacement/deletion
   safety and actual incremental cost. Stop before deployment; obtain any required
   later deployment authorization after review.
4. Qualify provider access only after disposable-account ownership, hard caps,
   and runtime budget enforcement are evidenced. Any later deployment, enablement,
   live calls, or publication needs its own recorded authorization and gates.

## Gate

The owner-approved pilot settings are recorded, but T11.2 is **not accepted**.
No authenticated preview or current deployment identity/state is available;
reservation values await verified pricing, and the configured Pulumi credentials
are inaccessible to this runtime. Do not run `pulumi up`, enable setup, or make
hosted pilot calls. T10.4 provider qualification is also **NOT RUN** because
disposable account ownership, hard caps, and runtime budget enforcement could
not be established. Do not make qualification requests until that evidence is
recorded. A reviewed preview is the next authorized preparation step; it does
not itself authorize deployment.

## Source reconciliation — 2026-09-30

The earlier packet incorrectly described the selected hosted route as OpenRouter
and treated the deployment target as unknown. Read-only review of Chat’s
`docs/aws-launch-2026-09-28.md`, `infra/lambda/Pulumi.prod.yaml`, and model routing
confirms the recorded Lambda target and Experiential route above. This is source
evidence, not a fresh cloud-state check. The client’s website-origin bug is being
corrected to reuse that existing API origin; no new host is introduced. Both
read-only health fetches failed at local DNS resolution, before an HTTP request.

## T11.2 cost reservation evidence — 2026-10-01

Public model hard limits and published standard prices are now recorded in
[`docs/evidence/T11.2-cost-reservations.md`](evidence/T11.2-cost-reservations.md).
The conservative public-limit envelope is 210,000 microUSD input plus 96,000
microUSD output and 96,000 microUSD reasoning per provider request, with
reasoning double-reserved as a safety allowance. This would permit only two
such full-envelope requests within the approved 1,000,000 microUSD daily cap.
The approved 10-session and 12-call settings are ceilings; a verified
fail-closed daily budget may admit fewer requests than those maxima. The
10-session limit is new-session admission per day, while each conversation's
12-call limit applies across its lifetime. A same-day cohort of 10 new sessions
using all 12 call reservations would be 120 requests; that is an illustrative
workload, not a global daily request ceiling. The T3.6 repair path reserves its
optional request under the same per-conversation call ceiling. Active sessions
from earlier days may also use quota, so the verified daily cost guard is the
global bound.

These are evidence-based candidate reservation values, not deployment-ready
settings: the deployed gateway route, account price tier, and enforcement of
Chat's `max_tokens=2048` request field have not been verified. The conditional
application-cap calculation and its limitations are in the evidence record.
No reservation values are applied to configuration, and setup remains disabled.
Do not enable or deploy until an owner-reviewed reservation policy and its
gateway/runtime enforcement are verified. The runtime may admit fewer calls
than these ceilings and must reject requests before crossing the daily cap.

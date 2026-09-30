# Shared chat launch authorization

Status: **draft for founder review; no live rollout authorization recorded**.
This packet distinguishes local code approval from cloud, provider-spend,
publication, and participant-data approval. Unknown values remain unset; this
record does not infer them from prior budgets.

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
| Existing backend target | Reuse the existing Chat service is the plan/ADR direction. | Name the actual stack, account, region, deployed origin, operator, and confirm that the target is the service covered by existing grants. Source notes conflict on whether the candidate Lambda stack has ever been applied; no authenticated account state was inspected. |
| Existing provider route | Candidate uses the existing Chat/OpenRouter route and existing secret reference. | Confirm the deployed model and current hard provider credit cap / total provider budget. Do not expose secret material. |
| Additional cloud resources | Local source prepares one setup-coordination table, TTL, narrowly scoped permissions, and additive Lambda configuration. | No Pulumi preview or founder authorization to apply these additions was supplied. No deletion/replacement safety or incremental monthly cost has been observed. |
| Setup admission and paid-call ceilings | No production setup value was selected. | Approve daily sessions and all five paid-call reservation values, plus a maximum incremental monthly spend. The recorded authorized incremental spend is currently **$0**; this is a stop boundary, not a proposed service budget. |
| Live smoke | No live endpoint/provider call is authorized by the source-only task results. | Define exact synthetic-only routes, request count, maximum provider spend, and who may run them after a reviewed preview/deployment. |
| Retention, email, and participant data | Candidate inherits existing Chat settings and intends to exclude setup from owner email. | Verify actual retention and backup/PITR settings, physical-expiry limits, email exclusion, participant notice/deletion path, and data policy against the selected stack. |
| Anza license and CLI publication | No approved public Anza license or CLI publication scope was found in the local decision record. | Select/record the license, release channel, package/artifact scope, signing-key custodian, and publication authorization. T9.4 provides code plumbing only; no production signing key or secret is configured. |

## Proposed rollout sequence

1. Resolve the actual existing Chat target and obtain a read-only authenticated
   Pulumi preview. Review every resource; require no deletion or replacement
   of existing transcript, Lambda, HTTP API, secret, and mail resources, no
   secret output, and only the expected additive table/policy/config changes.
2. Record an explicit maximum incremental monthly cloud spend, setup session
   cap, each paid-call reservation, total provider hard cap, smoke-call limit,
   and the operator. Keep setup disabled until all are approved and match the
   pinned catalog and immutable candidate image.
3. Only after preview review and authorization, deploy the backend with setup
   disabled. Run the approved synthetic legacy-route checks first; then enable
   setup within the reviewed caps and verify admission, expiry, deletion, and
   mail exclusion on the actual origin.
4. Publish the signed Anza CLI last, after license, artifact channel,
   production key custody, compatibility, and live-route checks are recorded.
   For rollback, disable setup first and preserve the existing website intake.

## Gate

The implementation and offline config checks are ready for review, but T11.2
is **not accepted**: there is no authenticated preview, verified deployment
identity/origin, selected cap or positive spend ceiling, live-smoke scope,
license decision, publication authorization, or production key custody in the
available evidence, and the configured Pulumi credentials are inaccessible to
this runtime. Do not run `pulumi up`, provision credentials, make paid provider
calls, enable setup, or publish until the missing scope is recorded and an
authorized preview can be run.

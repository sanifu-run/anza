# T10.3 security review evidence

Status: partial; certification remains `NOT RUN`. The local adversarial checks below passed. This evidence does not claim a complete security review, a shared Chat intake regression, or production verification.

## Scope reviewed

This task changed only `internal/acceptance/security_test.go` and this evidence file. It exercised local Anza boundaries through the exported ProjectBrief importer, archive extractor, catalog loader, and artifact downloader. No source guard, Chat file, plan/index, module dependency, provider, or live account was changed.

The tests currently cover poisoned credential fields in imported briefs, parent traversal, case-colliding archive entries, ZIP symlinks, unknown manifest fields, an unapproved artifact redirect, credential-canary redaction in a validation error, and review confirmation plus snapshot stability when a reviewed brief file changes after preview. `TestQuotaReplayIsolation` is present as an explicit skip because its behavior belongs to Chat T3.6 and the shared fixture cannot bind a socket in this runtime.

## Exact checks and outcomes

- Before adding these cases, `GOCACHE=/private/tmp/anza-t103-baseline GOTOOLCHAIN=local GOPROXY=off go test ./internal/acceptance -count=1` passed (`ok`, 1.289s). The six pre-existing journey/contract tests (`TestFreshJourney`, `TestExistingJourney`, `TestInterruptedJourney`, `TestBriefImportJourney`, `TestFeatureMismatchAndDisabledSetupAreActionable`, and `TestSharedChatContract`) skipped because `ANZA_CHAT_SOURCE` is unset.
- After implementation, `GOCACHE=/private/tmp/anza-t103-tests GOTOOLCHAIN=local GOPROXY=off go test ./internal/acceptance -run 'Test(MaliciousInputs|QuotaReplayIsolation|CredentialLeakCanaries|ApprovalTOCTOU|RedirectPolicy)$' -count=1 -v` passed. Malicious-input subtests (five), credential canary, approval snapshot, and redirect policy passed; quota replay was skipped with the reason above.
- Negative controls: with each relevant guard temporarily disabled in this isolated worktree, the paired test failed as predicted: catalog `DisallowUnknownFields` disabled → `manifest with an uncontracted fetch field was accepted`; ProjectBrief unknown-field rejection disabled → `brief containing an API key was accepted`; preview confirmation disabled → `unconfirmed preview was approved`; redirect-target validation disabled → the transport received a second request for `https://attacker.invalid/payload`. In all four cases, the test exited 1. I restored every temporarily modified source file byte-for-byte, then reran the corresponding tests green. These temporary source mutations are not part of this task diff.
- `GOCACHE=/private/tmp/anza-t103-tests GOTOOLCHAIN=local GOPROXY=off go vet ./internal/acceptance` passed.
- Full `GOCACHE=/private/tmp/anza-t103-tests GOTOOLCHAIN=local GOPROXY=off go test ./internal/acceptance -count=1 -v` passed: six pre-existing shared-Chat tests skipped because `ANZA_CHAT_SOURCE` is unset; the five local security test functions ran (quota replay explicitly skipped), with all runnable assertions green.
- `git diff --check` passed.
- Coordinator checked `uptime` (one-minute load 2.79) before attempting to
  claim the shared build lease for the contract's multi-package race test.
  `claim.sh claim R-build-lease` returned `BLOCKED: could not materialize
  empty tree` after a Git temporary-file permission error. No lease was
  acquired, so `go test -race ./internal/executor ./internal/state -count=1`
  was not run.

## Limits and unresolved evidence

The test-only redirect transport verifies host policy without network access; it does not qualify TLS, DNS, proxy, or operating-system behavior. The archive cases exercise local ZIP parsing but do not qualify Windows reparse-point behavior. No race detector ran. `TestApprovalTOCTOU` checks the reviewed brief snapshot/confirmation boundary; it does not prove stale-plan rejection at executor apply time.

Still unverified here: model-injection behavior in the shared Chat router; purpose/token isolation and durable quota replay; recommendation-version compatibility; delete and retry behavior; the original shared intake regression; stale-plan/apply races; process cancellation, privilege prompts, inverse config restoration, and OS credential lifetime. The local environment denies loopback binds, so the integrated Chat fixture cannot run. These checks need a runtime that can execute the approved cross-repository synthetic fixture and the appropriate single-lane race/package verification. No provider call or live service request was made. Findings outside the exercised local importer/archive/catalog/download cases remain open; this task does not clear the release security gate.

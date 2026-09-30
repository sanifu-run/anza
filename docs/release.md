# Release preparation and operation

This repository prepares Anza CLI binaries and their release metadata. The workflow does not create a Git tag, publish a GitHub Release, copy files to a production host, deploy the shared chat backend, or announce a release. Actual publication is owned by T11.3 and must follow the compatible-backend-first order in ADR 006.

## Artifact format

`scripts/release.sh build VERSION GOOS GOARCH OUTDIR` builds one target using the exact Go toolchain in `go.mod`, `CGO_ENABLED=0`, `-trimpath`, no VCS stamping, and an empty Go build ID. The allowed matrix is darwin, linux, and windows on amd64 and arm64. `scripts/release.ps1` offers the same single-target build on Windows PowerShell. The binary filename records the OS and architecture. Version is carried in the generated manifest and release metadata; the current CLI has no supported linker-injected version variable, so the packaging step does not claim that `anza version` reports the release version.

`scripts/release.sh assemble VERSION HTTPS_ORIGIN ARTIFACT_DIR OUTDIR` creates these payload manifests:

- `VERSION/manifest.json` for the POSIX bootstrap, with all four Darwin/Linux assets.
- `VERSION/windows/manifest.json` for PowerShell, with both Windows assets.
- `VERSION/scripts/install.sh` and `VERSION/scripts/install.ps1`, copied from the reviewed sources and stamped with the corresponding manifest SHA-256, version, and origin. Source installer files remain untouched. Their hashes are printed and included in `SHA256SUMS`; installers are deliberately outside their own manifests to avoid recursive digests.
- `VERSION/sbom.spdx.json`, an SPDX 2.3 dependency inventory, `VERSION/go-modules.json`, and `VERSION/dependency-licenses.txt`. The SBOM and inventory record dependency licenses as `NOASSERTION`; they are not a legal approval or bundled license texts. A release owner must resolve and publish applicable license notices before production release.
- `VERSION/release-metadata.json`, which records each payload-manifest digest, plus detached `release-metadata.sig` using Ed25519. `SHA256SUMS` covers the assembled files, including the signature and stamped installers.

The POSIX bootstrap fetches its manifest from `HTTPS_ORIGIN/VERSION/manifest.json`; its listed assets use `HTTPS_ORIGIN/anza/VERSION/`. The PowerShell bootstrap fetches `HTTPS_ORIGIN/anza/VERSION/windows/manifest.json`; Windows assets use the same versioned asset directory. The hosting origin must serve immutable paths over HTTPS without redirects, since the Windows bootstrap rejects redirects. The origin is supplied to the workflow at dispatch and is not a production endpoint endorsement. In a static-host staging tree, copy the candidate `VERSION/manifest.json` to `/VERSION/manifest.json`, the six `VERSION/anza-*` binaries to `/anza/VERSION/`, and `VERSION/windows/manifest.json` to `/anza/VERSION/windows/manifest.json`; copy the stamped scripts to the reviewed installer location. The workflow artifact preserves the complete candidate bundle and does not perform this copy.

The Ed25519 private key is supplied only as the protected `ANZA_ED25519_PRIVATE_KEY_PEM` environment secret. The matching public key is supplied as `ANZA_ED25519_PUBLIC_KEY_PEM`; assembly verifies the detached signature with it and fails on mismatch. Keep the signing environment protected with required reviewers, restrict secret access to the release job, and rotate keys through an explicit compatibility and revocation plan. Never commit either key. Signature validity does not by itself prove that the CLI updater enforces it.

## CI and preparation

Pull requests and pushes run Go tests and vet plus catalog/plan checks on native Ubuntu, macOS, and Windows runners. POSIX bootstrap fixtures run on Unix runners and PowerShell bootstrap fixtures run on Windows. A missing or failed native runner is unqualified; cross compilation is not a substitute for those checks.

To prepare a candidate, dispatch **Prepare release** with a semantic version and the reviewed HTTPS origin. The binary matrix is cross-built, then the `release-approval` environment gates signing and assembly. Configure required human reviewers and the two environment secrets in repository settings before using the workflow. Its output is a seven-day workflow artifact for review; it does not publish externally.

For a local packaging dry run, build all six target files into one directory with `scripts/release.sh build`, then provide temporary test signing keys via `ANZA_ED25519_PRIVATE_KEY_FILE` and `ANZA_ED25519_PUBLIC_KEY_FILE` when running `assemble`. Do not use production keys for local validation. Check all manifests and sums, verify the detached signature, run the bootstrap fixture suites, inspect the generated installer stamps, and compare repeated builds before requesting publication. Broad Go test/vet commands are required in CI and must follow the repository's shared build-lease instructions when run locally.

## Publication gate and recovery

Before T11.3 publication, a release owner must review native CI results, artifact hashes, signature verification, SBOM and license obligations, installer manifest digests, the actual HTTPS origin's redirect behavior, CLI/updater compatibility, catalog compatibility, and the current shared chat backend revision. Deploy the additive shared backend capability disabled, preserve and exercise legacy chat routes, enable compatible setup, then publish the matching CLI. T9.4 evidence does not satisfy these production checks.

If a candidate is suspect, stop distribution and prevent new downloads from its immutable version path. Preserve its metadata, checksums and audit record for investigation. Publish a new version for any correction; never overwrite versioned bytes. For signing-key compromise, disable the affected release path, mark the key revoked in the operator's trusted key distribution, rotate to a newly reviewed key, and require an updater that actually pins and checks the replacement key before accepting signed updates. The current Go updater exposes only an injected verifier interface and contains no production Ed25519 verifier or embedded public key, so signature verification is an explicit release blocker. The bootstrap scripts use the HTTPS-delivered stamped manifest digest as their initial trust root, not an independent signature check.

Backend rollback disables only the new setup surface while retaining legacy intake and existing conversation routes, as ADR 006 requires. Artifact rollback stops serving the candidate and restores the previously reviewed version through a new explicit version selection; it does not silently overwrite binaries or infer a downgrade migration. After any compromise, use the incident process to determine whether previously downloaded binaries or setup data require additional action.

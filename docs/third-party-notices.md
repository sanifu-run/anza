# Third-party notices and content provenance

This inventory is a release review aid, not a claim that every dependency or asset has cleared redistribution. Unresolved rights are release blockers.

## Anza content

The reviewed source audit in [content-inventory.md](content-inventory.md) found no tracked license or notice in the inspected founder skills checkout and no file-level redistribution license in the selected source files. Those texts are excluded unless written permission records modification, redistribution, attribution, and public/commercial-use scope. The ECC source audit identified MIT-licensed repository material at the audited revision; only the selected material covered by that license may be adapted, with required notices preserved. Dependencies, assets, later revisions, and unrelated ECC files require separate review. The inventory proposes original replacement skills; they are not blanket permission to copy the source prose.

The Anza catalog and generated content must have a file-level provenance record: author/source, exact revision where applicable, license/permission, required notice, adaptation status, and bundled destination. Missing or contradictory provenance blocks release. Do not remove existing notices from copied third-party material.

## Software, vendors, and services

Anza is a Go program and includes dependencies declared in `go.mod`/`go.sum`; this document does not certify their licenses or notices. Before redistribution, generate and review a complete dependency/license inventory for each target artifact, include required notices, and check vendored/generated assets separately. Installer tooling and vendor CLIs may have their own licenses and terms and are not represented as bundled Anza content unless the artifact inventory proves inclusion.

Hosted interview use reuses Sanifu chat, its configured model gateway/provider, AWS storage, and owner mail infrastructure. Those are service dependencies with separate privacy, billing, and operating terms, not licenses for redistribution of their software. See [privacy-draft.md](privacy-draft.md) for the observed data policy and release checks.

## Release blocker checklist

- Complete dependency and artifact inventory with required license texts/notices.
- Trace every bundled skill, recipe, exercise, template, and generated file to reviewed rights evidence.
- Exclude founder source files pending written permission; exclude unreviewed assets and transitive content.
- Record the final Anza license separately in [license-decision.md](license-decision.md).

No public package or content release is cleared by this inventory alone.

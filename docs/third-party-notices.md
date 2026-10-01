# Third-party notices and content provenance

Original Anza material is licensed under Apache License 2.0; see the repository `LICENSE` file for the license text. This grant covers original Anza-authored material only. It does not grant rights to material copied or adapted from third parties, excluded founder material, dependencies, vendor software, or hosted services.

## Anza content

The reviewed source audit in [content-inventory.md](content-inventory.md) found no tracked license or notice in the inspected founder skills checkout and no file-level redistribution license in the selected source files. Those texts remain excluded unless written permission records modification, redistribution, attribution, and public/commercial-use scope. The ECC source audit identified MIT-licensed repository material at the audited revision; only selected material covered by that license may be adapted, with its required MIT notice preserved. Dependencies, assets, later revisions, and unrelated ECC files require separate review. The inventory proposes original replacement skills; it is not blanket permission to copy source prose.

The Anza catalog and generated content must have file-level provenance: author/source, exact revision where applicable, license or permission, required notice, adaptation status, and bundled destination. Missing or contradictory provenance blocks release. Do not remove existing notices from copied third-party material.

## Software, vendors, and services

Anza is a Go program with dependencies declared in `go.mod` and `go.sum`; this document does not certify their licenses or notices. Before redistribution, generate and review a complete dependency/license inventory for each target artifact and include required notices. Check vendored and generated assets separately. No standalone image, font, or media bundle is declared by this inventory; recheck the actual release artifact before publication. Installer tooling and vendor CLIs may have their own licenses and terms and are not represented as bundled Anza content unless the artifact inventory proves inclusion.

Hosted interviews reuse Sanifu Chat, its configured model provider, AWS storage, and owner mail infrastructure. These are service dependencies with separate privacy, billing, and operating terms; Apache-2.0 does not license their software or waive those terms. See [privacy-draft.md](privacy-draft.md) for the observed data policy and release checks.

## Release blocker checklist

- Complete dependency and artifact inventory with required license texts and notices.
- Trace every bundled skill, recipe, exercise, template, and generated file to reviewed rights evidence.
- Exclude founder source files pending written permission; exclude unreviewed assets and transitive content.
- Preserve required upstream notices and confirm the Apache-2.0 `LICENSE` accompanies the release.

This inventory does not by itself clear a public package or content release.

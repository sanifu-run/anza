# Coordinator evidence: catalog contract extension

Date: 2026-09-28.

The user authorized both changes after the Wave 6 contract gaps were reported. This coordinator-owned follow-up is recorded separately from the original GPT-6-Luna T1.2/T5.1 certifications.

## Decisions

- A `Recipe` may omit `artifact` only when `install_strategy` is `manual` and `estimated_download_bytes` is zero. Automated strategies continue to require artifact metadata, and any present artifact still requires a source and digest. No artifact sentinel or implied downloadability is introduced.
- Exercise schema version 1 provides non-executable manual/unsupported scenarios selected by project kind and supported platform. Each scenario includes plain-language steps, missing capability IDs, readiness constraints and verification. No scenario can claim `ready` or carry executable command/URL/path fields.
- Catalog loading validates exercise schema, manifest checksum and provenance, scenario-platform predicates, dependency references, and cycles. Read-only accessors return deep copies.

## Owned interface changes

- `docs/contracts/v1.md`, `docs/contracts/schema-decisions.md`
- `schemas/recipe.schema.json`, `schemas/exercise.schema.json`
- `internal/domain/types.go`, `internal/domain/validate.go`
- `internal/catalog/catalog.go`
- contract fixtures and package tests
- task contracts T5.1, T5.3, T5.6 and T5.7 updated for the new boundary

## Validation

Coordinator implementation review on 2026-09-28:

- `go test ./internal/domain -count=1` — PASS; decoder, validation, fixtures, and artifact omission rules covered.
- `go vet ./internal/domain` — PASS.
- `go test ./internal/catalog -count=1` — PASS; loader checks, manual artifactless recipe, Exercise loading, platform rejection and copy isolation covered.
- `go vet ./internal/catalog` — PASS.
- Python `jsonschema` 4.26.0 Draft 2020-12 `check_schema` and valid/invalid recipe and Exercise fixture assertions — PASS.
- `python3 scripts/check-plan.py` and `git diff --check` — PASS.

These are local package/schema checks. No native installation, external catalog publication, Chat source change, or participant project was involved. The amendment makes the consumer interface available; T5.3 and T5.6 still need their own implementation and coordinator acceptance.

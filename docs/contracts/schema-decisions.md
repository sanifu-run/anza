# Contract schema decisions (v1 revision 2)

These schemas and Go value types materialize `docs/contracts/v1.md` revision 2. They are strict at the new Anza setup boundary. Unknown fields, duplicate object keys, malformed UTF-8, trailing JSON values and enum values outside the contract are rejected before values are used. Field validation errors include the relevant JSON field path and never echo whole payloads.

## Wire naming and mapping

Anza domain payloads use `snake_case`, including `ProjectBrief`, `MachineFacts`, `Recommendation`, `Recipe`, `Pack`, `Plan` and `Receipt`. The existing chat HTTP wrapper keeps its established `camelCase` names. The local `chat_setup_context_envelope` fixture exercises `requestId`, `expectedVersion`, `catalogVersion` and `machineFacts`; the nested `MachineFacts` object remains Anza snake_case. `SetupContextEnvelope` is an explicit Go wire adapter and is not itself one of the six published JSON schemas.

A read-only inspection of the peer chat checkout found `requestId` on the existing `/api/ask` request (`main.go` request type and inline ask fixtures such as `titles_test.go`). That checkout has no Anza setup fixture or current `expectedVersion`, `machineFacts` or `catalogVersion` wrapper to use as a source fixture. These three setup fields therefore follow the frozen Anza contract and are exercised with an Anza-owned synthetic wire fixture; no chat-owned file was added or changed. A paired chat fixture should be added by its owner when the setup route is implemented, then used by the cross-repository fixture task.

## Bounds and validation

- Project summaries use Unicode code-point counts: 1..4000; desired slice is at most 2000; project kind at most 200; constraints at most 20 entries of at most 500 characters; known stack at most 20 catalog IDs.
- Recommendation lists are capped at 30 recipe IDs and 10 pack IDs. Each explanatory collection is bounded to 100 items and 2000 characters per item. Recommendation fields named `command`, `url`, `path`, or any other undeclared key fail strict decoding.
- Recipe strategies are limited to `verified_archive`, `vendor_installer`, `package_manager` and `manual`; recipe and catalog identifiers use lowercase ASCII IDs. Download sizes must be nonnegative integers.
- Pack skill IDs must start with `anza-`; files, compatibility maps and ID collections have explicit count and string bounds.
- Plans preserve operation order, require logical target roots and safe relative paths, use lowercase SHA-256 digest strings, and use UTC RFC3339 timestamps with expiry after creation. The canonical plan digest omits its `digest` property, sorts object map keys, and preserves arrays.
- Receipt operation state and rollback state follow the contract's listed state values. Timestamps use UTC RFC3339. Fixtures use synthetic values only.

## Published schema files

`brief.schema.json` covers ProjectBrief and its optional learner-export source. `recommendation.schema.json`, `recipe.schema.json`, `pack.schema.json`, `plan.schema.json` and `receipt.schema.json` cover the corresponding top-level types. MachineFacts has a Go validator and appears nested in the wrapper fixture; the task's six-schema output list does not include a standalone MachineFacts schema. Approval and CheckResult have Go value types, strict decoders and canonical round-trip fixtures, but are not persisted as one of these six schema files.

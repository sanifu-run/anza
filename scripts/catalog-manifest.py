#!/usr/bin/env python3
"""Generate or verify the deterministic catalog manifest and safe setup export."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parent.parent
CATALOG = ROOT / "catalog"
MANIFEST_PATH = CATALOG / "manifest.json"
CATALOG_VERSION = "1.0.0"

RECIPE_PROVENANCE: dict[str, list[str]] = {
    "codex-cli": ["codex-cli-source", "codex-cli-apache-2.0"],
    "claude-code": ["claude-code-source", "claude-code-vendor-terms"],
    "git": ["git-source", "git-gpl-2.0"],
    "mobile-desktop-manual-review": [
        "mobile-desktop-anza-content",
        "mobile-desktop-product-rights-pending",
        "apple-xcode-support",
        "apple-xcode-sdk-terms",
        "android-studio-requirements",
        "android-sdk-terms",
    ],
}
NODE_PROVENANCE = ["nodejs-v24.21.0", "nodejs-platform-support", "nodejs-mit"]
MOBILE_EXERCISE_PROVENANCE = [
    "mobile-desktop-anza-content",
    "mobile-desktop-product-rights-pending",
    "apple-xcode-support",
    "apple-xcode-sdk-terms",
    "android-studio-requirements",
    "android-sdk-terms",
]

URL_PATTERN = re.compile(r"(?:https?://|www\.)", re.IGNORECASE)
ABSOLUTE_PATH_PATTERN = re.compile(r"(?:^|\s)(?:/[^\s]+|~/[^\s]+|[A-Za-z]:[\\/][^\s]+)")
COMMAND_PATTERN = re.compile(
    r"(?:^|\n)\s*(?:sudo\s+\S+|curl\s+(?:-[A-Za-z]|https?://)|wget\s+|"
    r"npm\s+(?:install|i|exec|run|ci|uninstall)\b|npx\s+|brew\s+(?:install|uninstall)\b|"
    r"apt(?:-get)?\s+(?:install|remove)\b|dnf\s+install\b|yum\s+install\b|apk\s+add\b|"
    r"winget\s+install\b|powershell\s+-|pwsh\s+-|bash\s+-|sh\s+-|"
    r"git\s+(?:clone|checkout|switch|pull|push|reset|clean)\b|"
    r"go\s+(?:install|run|build|test|get|tool|version|env|mod)\b|"
    r"python(?:3)?\s+(?:-m|-c|--version|[^\s]+\.py\b)|"
    r"node\s+(?:--version|[^\s]+\.(?:js|mjs|cjs)\b))",
    re.IGNORECASE,
)


class CatalogError(Exception):
    """A source catalog cannot be converted into a valid deterministic asset."""


def load_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise CatalogError(f"reading JSON {path.relative_to(ROOT)}: {exc}") from exc


def strings(values: Any, field: str, source: Path) -> list[str]:
    if not isinstance(values, list) or any(not isinstance(item, str) for item in values):
        raise CatalogError(f"{source.relative_to(ROOT)}: {field} must be a string array")
    return sorted(set(values))


def load_provenance() -> tuple[dict[str, dict[str, str]], dict[str, list[str]]]:
    records: dict[str, dict[str, str]] = {}
    skill_provenance: dict[str, list[str]] = {}
    for path in sorted((CATALOG / "provenance").glob("*.json")):
        value = load_json(path)
        if isinstance(value, list):
            rows = value
        elif isinstance(value, dict) and isinstance(value.get("provenance"), list):
            rows = value["provenance"]
            for asset in value.get("assets", []):
                if not isinstance(asset, dict):
                    raise CatalogError(f"{path.relative_to(ROOT)}: asset provenance row must be an object")
                asset_id = asset.get("id")
                provenance_id = asset.get("provenance_id")
                if not isinstance(asset_id, str) or not isinstance(provenance_id, str):
                    raise CatalogError(f"{path.relative_to(ROOT)}: asset requires id and provenance_id")
                skill_provenance[asset_id] = [provenance_id]
        else:
            raise CatalogError(f"{path.relative_to(ROOT)}: expected a provenance array or core provenance object")

        for row in rows:
            if not isinstance(row, dict):
                raise CatalogError(f"{path.relative_to(ROOT)}: provenance rows must be objects")
            record_id = row.get("id")
            source = row.get("source")
            license_text = row.get("license")
            if not all(isinstance(item, str) and item.strip() for item in (record_id, source, license_text)):
                raise CatalogError(f"{path.relative_to(ROOT)}: provenance requires non-empty id, source and license")
            if record_id in records:
                raise CatalogError(f"duplicate provenance id {record_id!r}")
            records[record_id] = {"id": record_id, "source": source, "license": license_text}

    return records, skill_provenance


def recipe_provenance(recipe_id: str) -> list[str]:
    if recipe_id in RECIPE_PROVENANCE:
        return RECIPE_PROVENANCE[recipe_id]
    if recipe_id.startswith("node-"):
        return NODE_PROVENANCE
    if recipe_id.startswith("go-"):
        return ["go-bsd-3-clause"]
    if recipe_id.startswith("python-"):
        return ["python-psf-2"]
    raise CatalogError(f"recipe {recipe_id!r} has no explicit provenance mapping")


def discover_assets(records: dict[str, dict[str, str]], skill_provenance: dict[str, list[str]]) -> list[dict[str, Any]]:
    entries: list[dict[str, Any]] = []
    seen_ids: set[str] = set()
    roots = (("recipes", ".json", "recipe"), ("packs", ".json", "pack"), ("exercises", ".json", "exercise"), ("skills", ".md", "skill"))

    for directory, suffix, kind in roots:
        base = CATALOG / directory
        for path in sorted(base.rglob("*")):
            if path.is_symlink():
                raise CatalogError(f"catalog asset cannot be a symlink: {path.relative_to(ROOT)}")
            if path.is_dir():
                continue
            relative_parts = path.relative_to(base).parts
            if any(part.startswith(".") for part in relative_parts) or "__pycache__" in relative_parts or path.suffix == ".pyc":
                continue
            if path.suffix != suffix:
                continue

            relative_path = path.relative_to(CATALOG).as_posix()
            if kind == "skill":
                asset_id = path.parent.name
                if path.name != "SKILL.md" or not asset_id.startswith("anza-"):
                    raise CatalogError(f"skill asset must be catalog/skills/anza-*/SKILL.md: {relative_path}")
                payload: dict[str, Any] | None = None
                provenance_ids = skill_provenance.get(asset_id)
                dependencies: list[str] = []
            else:
                payload_value = load_json(path)
                if not isinstance(payload_value, dict) or not isinstance(payload_value.get("id"), str):
                    raise CatalogError(f"{relative_path}: content must be an object with a string id")
                payload = payload_value
                asset_id = payload["id"]
                dependencies = strings(payload.get("prerequisites", []), "prerequisites", path) if kind in ("recipe", "pack") else []
                if kind == "recipe":
                    provenance_ids = recipe_provenance(asset_id)
                elif kind == "pack":
                    provenance_ids = sorted(set(strings(payload.get("provenance_ids"), "provenance_ids", path) + strings(payload.get("license_ids"), "license_ids", path)))
                elif asset_id == "mobile-desktop-exercise":
                    provenance_ids = MOBILE_EXERCISE_PROVENANCE
                else:
                    raise CatalogError(f"exercise {asset_id!r} has no explicit provenance mapping")

            if not asset_id or len(asset_id) > 100:
                raise CatalogError(f"{relative_path}: invalid or empty content id")
            if asset_id in seen_ids:
                raise CatalogError(f"duplicate catalog id {asset_id!r}")
            seen_ids.add(asset_id)
            if not provenance_ids:
                raise CatalogError(f"{relative_path}: no provenance references")
            for provenance_id in provenance_ids:
                if provenance_id not in records:
                    raise CatalogError(f"{relative_path}: unknown provenance id {provenance_id!r}")

            data = path.read_bytes()
            entries.append(
                {
                    "kind": kind,
                    "id": asset_id,
                    "path": relative_path,
                    "sha256": hashlib.sha256(data).hexdigest(),
                    "provenance_ids": sorted(set(provenance_ids)),
                    "dependencies": dependencies,
                }
            )

    return sorted(entries, key=lambda item: (item["kind"], item["id"]))


def build_manifest() -> dict[str, Any]:
    records, skill_provenance = load_provenance()
    entries = discover_assets(records, skill_provenance)
    return {
        "schema_version": 1,
        "version": CATALOG_VERSION,
        "entries": entries,
        "provenance": [records[key] for key in sorted(records)],
    }


def pretty_json(value: Any) -> bytes:
    return (json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode("utf-8")


def go_canonical_json(value: Any) -> bytes:
    compact = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
    compact = compact.replace("&", "\\u0026").replace("<", "\\u003c").replace(">", "\\u003e")
    compact = compact.replace("\u2028", "\\u2028").replace("\u2029", "\\u2029")
    return compact.encode("utf-8")


def catalog_digest(manifest: dict[str, Any]) -> str:
    normalized = dict(manifest)
    normalized["entries"] = [
        {
            "kind": entry["kind"],
            "id": entry["id"],
            "path": entry["path"],
            "sha256": entry["sha256"],
            "provenance_ids": sorted(set(entry["provenance_ids"])),
            "dependencies": sorted(set(entry["dependencies"])),
        }
        for entry in sorted(manifest["entries"], key=lambda item: (item["kind"], item["id"]))
    ]
    normalized["provenance"] = sorted(manifest["provenance"], key=lambda item: item["id"])
    return hashlib.sha256(go_canonical_json(normalized)).hexdigest()


def validate_exercise_for_export(value: dict[str, Any], source: Path) -> None:
    if value.get("schema_version") != 1 or not isinstance(value.get("scenarios"), list):
        raise CatalogError(f"{source.relative_to(ROOT)}: exercise is not schema version 1")
    allowed_exercise = {"schema_version", "id", "version", "description", "scenarios"}
    if set(value) != allowed_exercise:
        raise CatalogError(f"{source.relative_to(ROOT)}: unexpected Exercise fields")
    allowed_scenario = {
        "id", "project_kind", "supported_platforms", "status", "summary",
        "manual_steps", "missing_capability_ids", "readiness_constraints", "verification",
    }
    for scenario in value["scenarios"]:
        if not isinstance(scenario, dict) or set(scenario) != allowed_scenario:
            raise CatalogError(f"{source.relative_to(ROOT)}: unexpected Exercise scenario fields")
        if scenario.get("status") not in ("manual", "unsupported"):
            raise CatalogError(f"{source.relative_to(ROOT)}: Exercise status must remain manual or unsupported")


def safe_public_text(value: str, field: str) -> str:
    if URL_PATTERN.search(value) or ABSOLUTE_PATH_PATTERN.search(value) or COMMAND_PATTERN.search(value) or "`" in value:
        raise CatalogError(f"setup export {field} contains a URL, path, code span or command-like line")
    return value


def build_setup_catalog(manifest: dict[str, Any]) -> dict[str, Any]:
    entries = {entry["id"]: entry for entry in manifest["entries"]}
    recipes = []
    for recipe_path in sorted((CATALOG / "recipes").rglob("*.json")):
        recipe = load_json(recipe_path)
        recipe_id = recipe["id"]
        recipes.append(
            {
                "id": recipe_id,
                "version": recipe["version"],
                "description": safe_public_text(recipe["description"], f"recipes.{recipe_id}.description"),
                "purpose": safe_public_text(recipe["purpose"], f"recipes.{recipe_id}.purpose"),
                "supported_platforms": recipe["supported_platforms"],
                "sha256": entries[recipe_id]["sha256"],
            }
        )

    packs = []
    recipe_ids = {item["id"] for item in recipes}
    for pack_path in sorted((CATALOG / "packs").glob("*.json")):
        pack = load_json(pack_path)
        pack_id = pack["id"]
        packs.append(
            {
                "id": pack_id,
                "version": pack["version"],
                "recipe_ids": sorted(item for item in pack["prerequisites"] if item in recipe_ids),
                "skill_ids": sorted(pack["skill_ids"]),
                "sha256": entries[pack_id]["sha256"],
            }
        )

    exercises = []
    for exercise_path in sorted((CATALOG / "exercises").glob("*.json")):
        exercise = load_json(exercise_path)
        validate_exercise_for_export(exercise, exercise_path)
        exercise_id = exercise["id"]
        scenarios = []
        for scenario in exercise["scenarios"]:
            scenarios.append(
                {
                    "id": scenario["id"],
                    "project_kind": scenario["project_kind"],
                    "supported_platforms": sorted(scenario["supported_platforms"]),
                    "status": scenario["status"],
                    "missing_capability_ids": sorted(scenario["missing_capability_ids"]),
                    "readiness_constraints": [
                        safe_public_text(item, f"exercises.{exercise_id}.readiness_constraints")
                        for item in scenario["readiness_constraints"]
                    ],
                }
            )
        exercises.append(
            {
                "id": exercise_id,
                "version": exercise["version"],
                "description": safe_public_text(exercise["description"], f"exercises.{exercise_id}.description"),
                "scenarios": scenarios,
                "sha256": entries[exercise_id]["sha256"],
            }
        )

    return {
        "schema_version": 1,
        "catalog_version": manifest["version"],
        "catalog_digest": catalog_digest(manifest),
        "recipes": sorted(recipes, key=lambda item: item["id"]),
        "packs": sorted(packs, key=lambda item: item["id"]),
        "exercises": sorted(exercises, key=lambda item: item["id"]),
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument("--check", action="store_true", help="verify the checked-in manifest without writing")
    modes.add_argument("--setup-catalog", action="store_true", help="write sanitized setup metadata to stdout only")
    args = parser.parse_args()

    try:
        manifest = build_manifest()
        if args.setup_catalog:
            sys.stdout.buffer.write(pretty_json(build_setup_catalog(manifest)))
            return 0

        expected = pretty_json(manifest)
        if args.check:
            try:
                current = MANIFEST_PATH.read_bytes()
            except OSError as exc:
                raise CatalogError(f"reading {MANIFEST_PATH.relative_to(ROOT)}: {exc}") from exc
            if current != expected:
                print("catalog manifest drift detected", file=sys.stderr)
                return 1
            print("catalog manifest matches source assets")
            return 0

        MANIFEST_PATH.write_bytes(expected)
        print(f"wrote {MANIFEST_PATH.relative_to(ROOT)}")
        return 0
    except CatalogError as exc:
        print(f"catalog-manifest: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())

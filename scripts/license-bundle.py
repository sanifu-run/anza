#!/usr/bin/env python3
"""Assemble a deterministic, offline license bundle for an Anza release."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
from typing import NoReturn
import subprocess
import sys

LEGAL_NAME = re.compile(r"^(?:LICEN[CS]E|COPYING)(?:[._ -].*)?$|^(?:NOTICE|COPYRIGHT)(?:[._ -].*)?$", re.IGNORECASE)
LICENSE_TEXT_NAME = re.compile(r"^(?:LICEN[CS]E|COPYING)(?:[._ -].*)?$", re.IGNORECASE)


def fail(message: str) -> NoReturn:
    raise SystemExit(f"license bundle: {message}")


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def escaped_module_component(value: str) -> str:
    # Go module cache escaping maps uppercase ASCII to ! plus lowercase.
    return "".join("!" + c.lower() if "A" <= c <= "Z" else c for c in value)


def json_stream(raw: str) -> list[dict]:
    decoder = json.JSONDecoder()
    result = []
    offset = 0
    while offset < len(raw):
        while offset < len(raw) and raw[offset].isspace():
            offset += 1
        if offset == len(raw):
            break
        item, offset = decoder.raw_decode(raw, offset)
        if not isinstance(item, dict):
            fail("Go module listing contained a non-object record")
        result.append(item)
    return result


def git_revision(root: Path) -> str:
    try:
        revision = subprocess.check_output(
            ["git", "-C", str(root), "rev-parse", "--verify", "HEAD"],
            text=True, stderr=subprocess.DEVNULL,
        ).strip()
    except (OSError, subprocess.CalledProcessError):
        fail("source root must have a readable Git revision")
    if not re.fullmatch(r"[0-9a-f]{40,64}", revision):
        fail("source revision is not a full hexadecimal Git object ID")
    return revision


def go_toolchain_record(root: Path, outdir: Path) -> dict:
    try:
        env = os.environ.copy()
        env.update({"GOPROXY": "off", "GOSUMDB": "off", "GOWORK": "off", "GOTOOLCHAIN": "local"})
        output = subprocess.check_output(["go", "env", "GOROOT", "GOVERSION"], cwd=root, env=env,
                                         text=True, stderr=subprocess.PIPE)
    except FileNotFoundError:
        fail("Go toolchain is required to include its runtime license")
    except subprocess.CalledProcessError:
        fail("Go toolchain metadata could not be read")
    lines = output.splitlines()
    if len(lines) != 2 or not lines[0] or not re.fullmatch(r"go[0-9][A-Za-z0-9.+-]*", lines[1]):
        fail("Go toolchain metadata is incomplete")
    goroot = Path(lines[0])
    license_path = goroot / "LICENSE"
    # Homebrew relocates the Go distribution into <version>/libexec while
    # keeping its top-level license beside libexec. Accept that known layout
    # only when GOROOT itself has no license.
    if not license_path.exists() and goroot.name == "libexec":
        license_path = goroot.parent / "LICENSE"
    if license_path.is_symlink() or not license_path.is_file():
        fail("Go toolchain LICENSE is missing or unsafe")
    data = license_path.read_bytes()
    if not data.strip():
        fail("Go toolchain LICENSE is empty")
    relative = Path("licenses/go-toolchain/LICENSE")
    (outdir / relative).parent.mkdir(parents=True, exist_ok=True)
    (outdir / relative).write_bytes(data)
    return {"version": lines[1], "license_file": relative.as_posix(), "sha256": sha256(data)}


def dependency_records(root: Path, cache: Path) -> list[dict]:
    env = os.environ.copy()
    env.update({"GOPROXY": "off", "GOSUMDB": "off", "GOMODCACHE": str(cache), "GOWORK": "off", "GOTOOLCHAIN": "local"})
    try:
        raw = subprocess.check_output(
            ["go", "list", "-m", "-json", "all"], cwd=root, env=env,
            text=True, stderr=subprocess.PIPE,
        )
    except FileNotFoundError:
        fail("Go toolchain is required to enumerate the pinned module graph")
    except subprocess.CalledProcessError:
        # Do not leak cache paths from Go's diagnostic output into artifacts or logs.
        fail("offline Go module graph is incomplete or invalid; populate the module cache before release")
    records = []
    for item in json_stream(raw):
        if item.get("Main"):
            continue
        module_path, version = item.get("Path"), item.get("Version")
        if not isinstance(module_path, str) or not module_path or not isinstance(version, str) or not version:
            fail("Go module graph contains an unpinned dependency")
        if not isinstance(item.get("Sum"), str) or not item["Sum"]:
            fail(f"dependency {module_path}@{version} has no verified module checksum")
        if item.get("Replace"):
            replacement = item["Replace"]
            # Module-cache bundles only support immutable, versioned module replacements.
            # A local-directory replacement cannot be represented by the pinned module cache.
            if not isinstance(replacement.get("Version"), str) or not replacement["Version"]:
                fail(f"dependency {module_path}@{version} has an unpinned local replacement")
            source_path = replacement.get("Path")
            source_version = replacement["Version"]
        else:
            source_path, source_version = module_path, version
        module_dir = item.get("Dir")
        if not isinstance(module_dir, str) or not module_dir:
            fail(f"dependency {module_path}@{version} is missing from the offline module cache")
        try:
            resolved_cache = cache.resolve(strict=True)
            resolved_dir = Path(module_dir).resolve(strict=True)
            resolved_dir.relative_to(resolved_cache)
        except (OSError, ValueError):
            fail(f"dependency {module_path}@{version} is missing from the offline module cache")
        files = []
        for candidate in sorted(resolved_dir.iterdir(), key=lambda p: p.name.casefold()):
            if not LEGAL_NAME.fullmatch(candidate.name):
                continue
            if candidate.is_symlink() or not candidate.is_file():
                fail(f"dependency {module_path}@{version} has an unsafe legal-file entry")
            data = candidate.read_bytes()
            files.append({"name": candidate.name, "sha256": sha256(data), "data": data})
        if not any(LICENSE_TEXT_NAME.fullmatch(file["name"]) for file in files):
            fail(f"dependency {module_path}@{version} has no license text in the offline module cache")
        records.append({
            "module": module_path,
            "version": version,
            "sum": item.get("Sum", ""),
            "replace": ({"module": source_path, "version": source_version}
                        if item.get("Replace") else None),
            "files": files,
            "dir": resolved_dir,
        })
    if not records:
        fail("Go module graph contains no dependencies")
    records.sort(key=lambda row: (row["module"], row["version"]))
    return records


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", required=True, type=Path)
    parser.add_argument("--module-cache", required=True, type=Path)
    parser.add_argument("--outdir", required=True, type=Path)
    args = parser.parse_args()
    root = args.root.resolve(strict=True)
    cache = args.module_cache.resolve(strict=True)
    outdir = args.outdir.resolve()
    outdir.mkdir(parents=True, exist_ok=True)

    product_files = [
        (root / "LICENSE", "LICENSE"),
        (root / "NOTICE", "NOTICE"),
        (root / "docs/third-party-notices.md", "THIRD-PARTY-NOTICES.md"),
    ]
    product_hashes = {}
    for source, dest_name in product_files:
        if not source.is_file() or source.is_symlink():
            fail(f"required product notice is missing or unsafe: {dest_name}")
        data = source.read_bytes()
        if not data.strip():
            fail(f"required product notice is empty: {dest_name}")
        (outdir / dest_name).write_bytes(data)
        product_hashes[dest_name] = sha256(data)

    revision = git_revision(root)
    toolchain = go_toolchain_record(root, outdir)
    modules = dependency_records(root, cache)
    module_manifest = []
    sbom_packages = []
    license_lines = [
        "Anza release dependency license files",
        "",
        "These are exact files copied from pinned Go module cache entries.",
        "No SPDX license expression is inferred from a filename.",
        f"\nGo toolchain {toolchain['version']}",
        f"  {toolchain['license_file']} sha256:{toolchain['sha256']}",
    ]
    for index, module in enumerate(modules, 1):
        safe_path = "/".join(escaped_module_component(part) for part in module["module"].split("/"))
        bundle_dir = Path("licenses/go") / f"{safe_path}@{escaped_module_component(module['version'])}"
        manifest_files = []
        license_lines.append(f"\n{module['module']} {module['version']} (sum: {module['sum'] or 'unavailable'})")
        for file in module["files"]:
            relative = bundle_dir / file["name"]
            (outdir / relative).parent.mkdir(parents=True, exist_ok=True)
            (outdir / relative).write_bytes(file["data"])
            entry = {"path": relative.as_posix(), "sha256": file["sha256"]}
            manifest_files.append(entry)
            license_lines.append(f"  {relative.as_posix()} sha256:{file['sha256']}")
        module_manifest.append({
            "module": module["module"], "version": module["version"], "sum": module["sum"],
            "replacement": module["replace"], "license_files": manifest_files,
        })
        sbom_packages.append({
            "SPDXID": f"SPDXRef-Package-{index}", "name": module["module"],
            "versionInfo": module["version"], "downloadLocation": "NOASSERTION",
            "filesAnalyzed": False, "licenseConcluded": "NOASSERTION",
            "licenseDeclared": "NOASSERTION", "copyrightText": "NOASSERTION",
            "externalRefs": [{"referenceCategory": "PACKAGE-MANAGER", "referenceType": "purl",
                               "referenceLocator": f"pkg:golang/{module['module']}@{module['version']}"}],
        })
    (outdir / "go-modules.json").write_text(json.dumps(module_manifest, sort_keys=True, indent=2) + "\n")
    (outdir / "dependency-licenses.txt").write_text("\n".join(license_lines) + "\n")
    sbom_packages.append({
        "SPDXID": f"SPDXRef-Package-{len(sbom_packages) + 1}",
        "name": "Go toolchain", "versionInfo": toolchain["version"],
        "downloadLocation": "NOASSERTION", "filesAnalyzed": False,
        "licenseConcluded": "NOASSERTION", "licenseDeclared": "NOASSERTION",
        "copyrightText": "NOASSERTION",
    })
    sbom = {
        "spdxVersion": "SPDX-2.3", "dataLicense": "CC0-1.0", "SPDXID": "SPDXRef-DOCUMENT",
        "name": "anza-release", "documentNamespace": f"https://spdx.org/spdxdocs/anza/{revision}",
        "creationInfo": {"creators": ["Tool: scripts/license-bundle.py"], "created": "2000-01-01T00:00:00Z"},
        "packages": sbom_packages,
    }
    (outdir / "sbom.spdx.json").write_text(json.dumps(sbom, sort_keys=True, indent=2) + "\n")
    bundle = {
        "source_revision": revision,
        "product_files": product_hashes,
        "go_toolchain": toolchain,
        "dependencies": module_manifest,
    }
    (outdir / "license-bundle.json").write_text(json.dumps(bundle, sort_keys=True, indent=2) + "\n")


if __name__ == "__main__":
    main()

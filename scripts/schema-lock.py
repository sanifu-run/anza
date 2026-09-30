#!/usr/bin/env python3
"""Check or update SHA256SUMS for the repository JSON schema files."""
import argparse
import hashlib
from pathlib import Path
import sys


def entries(schema_dir: Path):
    return {
        path.name: hashlib.sha256(path.read_bytes()).hexdigest()
        for path in sorted(schema_dir.glob("*.schema.json"))
    }


def render(values):
    return "".join(f"{digest}  {name}\n" for name, digest in sorted(values.items()))


def check(schema_dir: Path) -> bool:
    lock_path = schema_dir / "SHA256SUMS"
    expected = render(entries(schema_dir))
    try:
        current = lock_path.read_text()
    except FileNotFoundError:
        current = ""
    if current != expected:
        print(f"schema lock is stale: run {Path(__file__).name} --write", file=sys.stderr)
        return False
    print(f"schema lock matches {len(entries(schema_dir))} files")
    return True


def main():
    parser = argparse.ArgumentParser()
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--write", action="store_true", help="write the schema lock manifest")
    mode.add_argument("--check", action="store_true", help="check the schema lock (default)")
    parser.add_argument("--schemas", type=Path, default=Path(__file__).resolve().parents[1] / "schemas", help=argparse.SUPPRESS)
    args = parser.parse_args()
    schema_dir = args.schemas.resolve()
    if not schema_dir.is_dir():
        parser.error(f"schema directory does not exist: {schema_dir}")
    if args.write:
        (schema_dir / "SHA256SUMS").write_text(render(entries(schema_dir)))
        print(f"wrote {schema_dir / 'SHA256SUMS'}")
        return 0
    return 0 if check(schema_dir) else 1


if __name__ == "__main__":
    raise SystemExit(main())

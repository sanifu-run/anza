#!/usr/bin/env python3
"""Prove schema lock check rejects a changed schema in an isolated fixture."""
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

root = Path(__file__).resolve().parents[2]
with tempfile.TemporaryDirectory(prefix="anza-schema-lock-") as temp:
    fixture = Path(temp) / "schemas"
    shutil.copytree(root / "schemas", fixture)
    result = subprocess.run(
        [sys.executable, str(root / "scripts/schema-lock.py"), "--check", "--schemas", str(fixture)],
        capture_output=True,
        text=True,
    )
    if result.returncode:
        raise SystemExit(f"baseline schema lock failed: {result.stderr}{result.stdout}")
    schema = next(fixture.glob("*.schema.json"))
    schema.write_bytes(schema.read_bytes() + b"\n")
    result = subprocess.run(
        [sys.executable, str(root / "scripts/schema-lock.py"), "--check", "--schemas", str(fixture)],
        capture_output=True,
        text=True,
    )
    if result.returncode == 0:
        raise SystemExit("negative control failed: changed schema was accepted")
print("schema lock negative control rejected a changed schema")

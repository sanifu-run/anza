#!/bin/sh
# Exercise release.sh build routing with a fake Go command; no compiler runs.
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
release_script=${ANZA_RELEASE_SCRIPT:-"$repo/scripts/release.sh"}

python3 - "$release_script" <<'PY'
import base64
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

release_script = Path(sys.argv[1]).resolve(strict=True)
public_key = base64.b64encode(bytes(32)).decode("ascii")
expected_tags = "keyring_no1password,keyring_noprotonpass,keyring_nofile,keyring_nopass"

with tempfile.TemporaryDirectory(prefix="anza-release-build-routing-") as temporary:
    root = Path(temporary)
    fake_bin = root / "bin"
    fake_bin.mkdir()
    capture = root / "go-build.json"
    fake_go = fake_bin / "go"
    fake_go.write_text(
        "#!/usr/bin/env python3\n"
        "import json, os, pathlib, sys\n"
        "args=sys.argv[1:]\n"
        "if args == ['env','GOHOSTOS']: print(os.environ['FIXTURE_HOST_OS'])\n"
        "elif args == ['env','GOHOSTARCH']: print(os.environ['FIXTURE_HOST_ARCH'])\n"
        "elif args and args[0] == 'build':\n"
        " pathlib.Path(os.environ['FIXTURE_CAPTURE']).write_text(json.dumps({'args':args,'cgo':os.environ.get('CGO_ENABLED'),'goos':os.environ.get('GOOS'),'goarch':os.environ.get('GOARCH')}))\n"
        " pathlib.Path(args[args.index('-o')+1]).write_bytes(b'synthetic artifact')\n"
        "else: raise SystemExit('unexpected fake go invocation: '+repr(args))\n",
        encoding="utf-8",
    )
    fake_go.chmod(0o755)
    fake_clang = fake_bin / "clang"
    fake_clang.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
    fake_clang.chmod(0o755)

    def invoke(goos: str, goarch: str, host_os: str, host_arch: str, output: Path):
        environment = os.environ.copy()
        environment.update({
            "PATH": str(fake_bin) + os.pathsep + environment.get("PATH", ""),
            "ANZA_RELEASE_PUBLIC_KEY_BASE64": public_key,
            "FIXTURE_HOST_OS": host_os,
            "FIXTURE_HOST_ARCH": host_arch,
            "FIXTURE_CAPTURE": str(capture),
        })
        return subprocess.run(
            [str(release_script), "build", "4.5.6", goos, goarch, str(output)],
            text=True, capture_output=True, env=environment, check=False,
        )

    mismatch_output = root / "darwin-mismatch"
    mismatch = invoke("darwin", "arm64", "linux", "arm64", mismatch_output)
    assert mismatch.returncode != 0 and "matching macOS architecture" in mismatch.stderr
    assert not mismatch_output.exists(), "mismatched Darwin request created output before failing"
    assert not capture.exists(), "mismatched Darwin request reached the fake compiler"

    for goos, arches in (("darwin", ("amd64", "arm64")),
                         ("linux", ("amd64", "arm64")),
                         ("windows", ("amd64", "arm64"))):
        for goarch in arches:
            host_os = "darwin" if goos == "darwin" else "linux"
            result = invoke(goos, goarch, host_os, goarch, root / f"{goos}-{goarch}")
            assert result.returncode == 0, f"{goos}/{goarch} failed: {result.stderr}"
            record = json.loads(capture.read_text(encoding="utf-8"))
            args = record["args"]
            linker_flags = next(arg for arg in args if arg.startswith("-ldflags="))
            assert f"main.version=4.5.6" in linker_flags
            assert "main.buildTime=" not in linker_flags, "build timestamp must remain deterministic"
            assert record["goos"] == goos and record["goarch"] == goarch
            if goos == "darwin":
                assert record["cgo"] == "1"
                assert "-tags" in args and args[args.index("-tags") + 1] == expected_tags
            else:
                assert record["cgo"] == "0"
                assert "-tags" in args and args[args.index("-tags") + 1] == expected_tags
            assert (root / f"{goos}-{goarch}" / (f"anza-{goos}-{goarch}" + (".exe" if goos == "windows" else ""))).is_file()

print("release build routing passed: native Darwin cgo/host guard, Linux/Windows cgo-off tags, and version injection")
PY

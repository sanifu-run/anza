# T10.2 platform qualification

Status: preparation only. **No platform is qualified by this record.** All
cells below remain `unqualified` until lifecycle evidence exists from a
matching disposable native account or VM. Cross-compilation and fixtures do
not qualify an OS or architecture.

## Execution boundary

This run used macOS 26.6.2 arm64, branch `task/T10.2-luna`, base `98cbc5e`. The
host is not declared disposable. No bootstrap, vendor installer, download,
PATH/configuration mutation, or real Anza install was run. Neither `pwsh` nor
`powershell` is installed, so the PowerShell helper could not be executed.
These facts are not qualification evidence.

## Platform matrix

Each of the 24 vendor/platform cells from `docs/compatibility.md` is listed
below and remains unqualified. Run the seven proposed pilot targets natively
or in matching disposable native VMs. Windows ARM, WSL, other glibc Linux, and
Alpine/musl remain outside the pilot; they are explicit unqualified cells, not
implied passes. Do not infer support for any other OS release.

| Target ID | Native target | Claude Code | Codex CLI | Evidence |
| --- | --- | --- | --- | --- |
| `macos-arm64` | macOS arm64 | unqualified | unqualified | pending native run |
| `macos-amd64` | macOS amd64 | unqualified | unqualified | pending native run |
| `windows-amd64` | Windows 11 amd64, native | unqualified | unqualified | pending native run |
| `windows-arm64` | Windows 11 arm64, native | unqualified | unqualified | outside pilot; no native run |
| `wsl1` | WSL 1 | unqualified | unqualified | outside pilot; no native run |
| `wsl2-windows-amd64` | WSL 2 on Windows amd64 | unqualified | unqualified | outside pilot; no native run |
| `ubuntu-2204-amd64` | Ubuntu 22.04 amd64 | unqualified | unqualified | pending native run |
| `ubuntu-2404-amd64` | Ubuntu 24.04 amd64 | unqualified | unqualified | pending native run |
| `debian-12-amd64` | Debian 12 amd64 | unqualified | unqualified | pending native run |
| `debian-12-arm64` | Debian 12 arm64 | unqualified | unqualified | pending native run |
| `other-glibc-linux` | Other glibc Linux x86_64/arm64 | unqualified | unqualified | outside pilot; exact distro needed |
| `alpine-musl` | Alpine Linux 3.19+ / musl | unqualified | unqualified | outside pilot; no native run |

The Anza bootstrap itself currently publishes macOS/Linux amd64/arm64 and
Windows amd64/arm64 artifacts; that artifact matrix does not establish vendor
compatibility for every row above. Keep Anza bootstrap evidence and each
Codex/Claude vendor method recorded separately.

## Read-only snapshot helpers

The POSIX and PowerShell scripts hash only explicit file paths. They do not
invoke Anza, vendor installers, package managers, or network requests; change
PATH/configuration; or remove files. Output includes category, operator label,
file state, and SHA-256, with file contents and paths omitted. Use a fresh
evidence directory in the disposable VM for each target/scenario; never supply
secret files. The helpers refuse to overwrite an existing snapshot.

Example for POSIX (replace example paths with reviewed paths):

```sh
scripts/qualify-platform.sh --phase before --target macos-arm64 \
  --evidence-dir /tmp/anza-evidence/macos-arm64-first \
  --owned anza_binary="$HOME/.local/bin/anza" \
  --unowned codex_config="$HOME/.codex/config.toml"
scripts/qualify-platform.sh --phase after --target macos-arm64 \
  --evidence-dir /tmp/anza-evidence/macos-arm64-first \
  --owned anza_binary="$HOME/.local/bin/anza" \
  --unowned codex_config="$HOME/.codex/config.toml"
```

PowerShell takes matching `-Phase`, `-Target`, `-EvidenceDirectory`, `-Owned`,
and `-Unowned` arguments. Compare snapshots and record the decision plus
sanitized command results. Hashes are supporting evidence; they do not show
whether a change is semantically correct.

## Lifecycle scenarios

Use a fresh disposable account/VM for each target. Record OS build, shell,
architecture, Anza bootstrap revision and digest, vendor/method/version,
privilege level, network state, sanitized command and exit status. Use a
synthetic project/config and no credentials. Capture owned and unowned
before/after digests. Inspect changes inside the disposable VM and record the
result without copying file contents.

| Scenario | Procedure and pass evidence |
| --- | --- |
| Fresh first install | From a clean VM snapshot, run Anza bootstrap and each selected vendor installer as the standard user. Record versions/status and owned/unowned digests; confirm no admin prompt or machine-wide write. |
| Repeat install | Repeat the same pinned install. Confirm clear success/no-op, expected version, and no unrelated changes. Capture another digest pair. |
| Existing custom config | Seed harmless vendor config with unrelated keys/comments. Install or update managed entries. Confirm unrelated settings remain intact; record conflict behavior if the tool refuses a merge. |
| PATH restart | Record sanitized PATH entry count/order, accept only a disclosed per-user change if offered, then open a new shell. Confirm command resolution and preservation of unrelated entries. Never paste a full PATH. |
| Interrupted vendor install | Interrupt at a reproducible safe point in the disposable VM. Record partial state. Inspect before retry; confirm no false success and no unrelated changes. |
| Update | Install a pinned earlier supported version, retain custom config and unrelated files, then update. Confirm version, owned-change scope, and documented recovery/backup behavior. |
| Uninstall | Use Anza's documented removal flow. Confirm only Anza-owned files/settings are removed; user config and vendor login remain; shared vendor binaries are retained unless recorded as Anza-owned. |
| Non-admin | Repeat install/update/removal as a standard user with no elevation. Confirm system locations and machine PATH stay untouched. |
| Offline/cached path | Stage reviewed digest-verified artifacts in the VM, disconnect networking, and run the documented offline path. Record success or accurate stop conditions. A mock endpoint/fixture is not vendor qualification. |

Record Codex and Claude separately. Provider login and paid requests are out of
scope; do not authenticate or send model requests. T10.4 owns live provider
qualification.

## Result template

Copy one block per target and scenario into the sanitized handoff. Keep state
`unqualified` until every required scenario passes on a matching native target
and a reviewer checks its evidence.

```text
Target ID / OS release / architecture:
Scenario:
State: unqualified
Disposable native account/VM identifier (non-sensitive):
Anza bootstrap revision and manifest/artifact digest:
Vendor and method/version:
Sanitized command / exit status:
Owned before/after snapshot files:
Unowned before/after snapshot files:
Preservation result:
Failure/recovery result:
Evidence class: native disposable account/VM
Reviewer and review date:
```

## Remaining gates

Existing `scripts/tests/bootstrap-posix.sh` and
`scripts/tests/bootstrap-windows.ps1` exercise controlled fixtures, not native
vendor installs. No lifecycle scenario has been run for this task. The
coordinator must provision disposable native targets, execute and review the
matrix, and update platform classifications. No certification is asserted.

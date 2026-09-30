# Participant guide (pre-release)

Anza is a local Go command-line program intended to guide a participant through a project setup. The current source has a guided `setup` flow, but there is no qualified public installer or production Anza release yet. The shared interview backend and its setup capability must also be enabled and verified before a hosted interview can be relied on. Until those gates are complete, treat this guide as an implementation preview; do not use it as evidence that Anza installs on every computer.

## Before starting

Bring a project you are allowed to discuss and decide which small, non-sensitive facts would help plan its next step. Do not submit source files, credentials, customer records, private URLs or other secrets. A paying customer is not required, and Anza does not promise earnings, completed customer delivery, or a particular project result.

Anza does not require a Go compiler on a participant machine once a signed, qualified release exists. No such release is documented here yet. The repository has POSIX shell bootstrap code for macOS/Linux and a native PowerShell bootstrap for Windows, but both still depend on release-stamped artifacts. No public installer URL or participant install command is available yet. Supported operating systems, installer integrity, supported tool versions, and native setup results remain release qualifications; Windows and other platforms are not declared qualified by source-code support alone.

## Start and review

After obtaining a release through a future approved distribution channel, open a terminal in the project and run `anza help` to see commands present in that build, then `anza version` to identify it. The current development CLI also accepts `anza setup` for its guided interview, plan, review, apply, login guidance, and checks. No-argument invocation enters that flow only when stdin and stdout are terminals; otherwise it prints help. Hosted interview availability depends on the server capability and can be unavailable even when local commands work.

Read each question, share only project facts you approve, and inspect the proposed plan before applying it. Review shows the effects and digest of the exact local plan. Explicit plan approval authorizes those listed local changes only; a paid model request requires its own confirmation. Skip or stop when a recommendation is unclear. Record provider choice per agent: Codex and Claude subscription login are separate from OpenRouter credentials, and each OpenRouter key belongs to the participant. Never paste a key into a project brief or ordinary chat. OpenRouter launch credentials are kept in the operating system credential store; direct subscription login remains with the vendor.

Anza's supported path is a reviewed, project-specific setup and a meaningful first check. Some recommendations will be manual or unsupported. A vendor SDK may require its own download, license acceptance, platform, account, signing step, or device. Anza does not promise universal automatic SDK installation.

Useful current commands include `anza inspect` for local workspace facts, `anza doctor` for readiness, `anza repair` after an interrupted apply, `anza exercise --id ID --project-kind KIND` for a catalogued local project check, `anza diagnostics --preview` to inspect a redacted local report, `anza diagnostics --export FILE` to export it, and `anza diagnostics --delete-local-session NAME` to remove a named local session. Check `anza help` in the installed build for exact supported arguments. Review a diagnostic export before sharing it.

## Interview data and stopping

The planned shared interview reuses Sanifu chat. The current Anza consent prompt is not consent to email an owner a transcript. If setup mode is unavailable, continue with local inspection and manual steps or stop; do not retry paid requests after an ambiguous result. Use `anza interview` to continue a saved local setup interview when that command is available in the build. For deletion, use the diagnostics/session deletion command shown by that build; hosted conversation deletion and its limits are described in [the privacy draft](privacy-draft.md).

For errors and concrete recovery actions, see [troubleshooting](troubleshooting.md). Vendor installation methods and platform evidence are recorded in [compatibility.md](compatibility.md).

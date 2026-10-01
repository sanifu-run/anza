# Participant guide (pre-release)

Anza is a local Go command-line program for guiding a participant through a project setup. Its current development CLI has a guided `setup` flow. There is no qualified public installer or production Anza release yet, and hosted interview availability depends on the shared Sanifu Chat setup capability being enabled. Treat this as an implementation preview; it does not qualify installation or operation on every computer.

## Before starting

Bring a project you are allowed to discuss and decide which small, non-sensitive facts could help plan its next step. Do not submit source files, credentials, customer records, private URLs, or other secrets. A paying customer is not required, and Anza does not promise earnings, completed customer delivery, or a particular project result.

No public installer URL or participant install command is available yet. Although the repository has shell bootstrap code for macOS/Linux and PowerShell bootstrap code for Windows, both depend on release-stamped artifacts. Supported operating systems, installer integrity, supported tool versions, and native setup results remain release qualifications. Source support alone does not qualify a platform.

## Start and review

After obtaining a future approved release, run `anza help` to see commands present in that build and `anza version` to identify it. In the current development CLI, `anza setup` starts the guided flow. With no command, that flow starts only when both stdin and stdout are terminals; otherwise Anza prints help. A working local CLI does not establish that the hosted interview is available.

Read each question, share only facts you approve, and inspect the proposed plan before applying it. Review shows the effects and digest of the local plan. Approval covers only those listed local changes; a paid model request requires its own confirmation. Skip or stop if a recommendation is unclear.

Provider choices belong to each participant and agent. Logging into a participant's ChatGPT or Claude subscription is separate from using OpenRouter: an OpenRouter key is the participant's credential for an agent provider route and is stored in the operating system credential store. The hosted Anza interview is a separate service: its selected `gpt-6-luna` route uses Experiential Labs. A participant's subscription or OpenRouter key does not authorize or configure that hosted interview. Never put a key in a project brief or ordinary chat.

Some recommendations will require manual work or may be unsupported. A vendor SDK may require a separate download, license acceptance, platform, account, signing step, or device. Anza does not promise automatic SDK installation.

Useful development commands include `anza inspect` for local workspace facts, `anza doctor` for readiness, `anza repair` after an interrupted apply, `anza exercise --id ID --project-kind KIND` for a catalogued local check, `anza diagnostics --preview`, `anza diagnostics --export FILE`, and `anza diagnostics --delete-local-session NAME`. Check `anza help` for the exact options in your build. Review a diagnostic export before sharing it.

## Interview data and stopping

The hosted interview reuses Sanifu Chat. Its current AWS transcript copy has a configured 30-day TTL after its last update; expired records become hidden at the expiry boundary and are physically removed asynchronously. Service logs have a separate 14-day retention period. The selected hosted `gpt-6-luna` route uses Experiential Labs, whose production prompt capture has no fixed expiry. These service/provider copies are separate from local CLI state and from participant-owned subscription accounts. See the [privacy draft](privacy-draft.md) for details and limits.

Setup-purpose conversations are excluded from scheduled owner transcript email. Anza's setup consent is not consent to email an owner a transcript. If setup is unavailable, continue with local inspection and manual steps or stop. Do not retry a paid request after an ambiguous result. Use `anza interview` only if that command is present in your build to resume a saved local setup interview. Use the diagnostics local-session deletion option to remove a named local session; hosted deletion and its limits are described in the privacy draft.

For recovery steps, see [troubleshooting](troubleshooting.md). Vendor installation methods and platform evidence are recorded in [compatibility.md](compatibility.md). The original Anza material is licensed under Apache-2.0; the release still requires complete third-party dependency and bundled-content notices. See [third-party notices](third-party-notices.md).

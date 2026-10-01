# Troubleshooting (development CLI)

Use the command supported by your installed build; `anza help` lists its commands. These steps describe the current source behavior and do not establish that a public release or hosted service is available.

| What you see | What to do |
| --- | --- |
| The command is unknown or an option is rejected | Run `anza help` and use the syntax shown by that build. The development CLI's exercise form is `anza exercise --id ID --project-kind KIND [--json]`. |
| No-argument invocation prints help instead of starting setup | Start `anza setup` explicitly. The implicit guided flow requires both stdin and stdout to be terminals. |
| Setup or hosted interview is unavailable | Continue with local `anza inspect` and manual steps, or stop. Hosted operation requires the shared server capability; a local CLI response cannot enable it. |
| A hosted or paid request times out or returns an ambiguous result | Do not repeat the paid request until its result is understood. Continue locally or stop; contact the project operator through the established support channel when one is provided. |
| An apply was interrupted | Run `anza repair` and follow the current approval prompt. If it asks for a fresh review, run `anza review` and inspect the new plan before applying. |
| Apply says there is no current approval or the plan changed | Run `anza review` again and approve the current digest before retrying `anza apply`. Approval for an earlier plan does not authorize a changed plan. |
| A readiness check fails | Run `anza doctor` and use its reported checks to decide which manual prerequisite to address. “Ready” describes only the checks reported for that machine and plan. |
| You need to inspect or share diagnostics | Run `anza diagnostics --preview`, then `anza diagnostics --export FILE` if needed. Review the exported file and remove sensitive details before sharing. |
| You need to remove a local interview session | Use `anza diagnostics --delete-local-session NAME` for the named local session. This does not delete a hosted conversation or provider data. |
| Hosted conversation deletion is needed | Use the conversation's recovery token and the shared conversation deletion flow. See the [privacy draft](privacy-draft.md) for what deletion leaves behind. |
| The recommended tool or SDK cannot be installed | Follow the vendor's documented manual installation and license steps, or skip it. Anza does not guarantee automatic installation or support for every platform. |

Do not interpret “ready” as customer acceptance, commercial success, or completion of all project work. It means only the checks reported for that specific machine and plan.

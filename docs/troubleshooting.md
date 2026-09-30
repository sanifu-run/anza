# Troubleshooting

Use the action for the condition you actually see. The command list can change between development builds; run `anza help` and `anza version` first. A local command being present does not mean a public installer or hosted setup service is available.

| State | What to do |
| --- | --- |
| Shared setup mode is disabled or unavailable | Stop hosted interview attempts. Run `anza inspect` and `anza doctor` for local information, then continue with manual steps or wait for the operator to announce a qualified service window. Do not retry an ambiguous paid request. |
| A recommendation needs a catalog item that is unsupported | Keep the item manual. Run `anza doctor` to see per-tool readiness and use the vendor's documented setup procedure; do not substitute an unreviewed command. |
| Installation or apply was interrupted | Run `anza repair` from the same project/workspace and follow its reconciliation report. Check the observed installed state before retrying. System package installs may need manual recovery. |
| Credential store/keychain is locked or unavailable | Unlock the OS credential store using its normal desktop settings, then retry `anza run codex` or `anza run claude` when using OpenRouter. If that store is unavailable, choose the vendor's native subscription login or stop; never put the key in a project file. |
| Existing configuration conflicts with the reviewed change | Stop and inspect the file with your normal editor. Preserve your changes, then rerun `anza setup` to produce a new plan and review it. Do not force an old digest or overwrite the conflict. |
| `anza` is not found (`PATH` issue) | Open a new terminal after installation. Check whether the installer disclosed a per-user PATH change; if not, add the displayed install directory to your user PATH using your OS settings, then run `anza version`. Do not use an elevated shell to bypass this. |
| Provider quota or setup limit is exhausted | Stop and wait for the stated reset or Do not switch accounts or repeatedly submit the same request to evade a limit. Local inspection remains available. |
| The interview was interrupted or cannot resume | Run `anza interview` to continue the saved local interview if offered. If it reports expired or missing session state, start a new `anza setup` and share only reviewed facts again. A recovery token is not a browser login or a way to recover another person's session. |
| You want to remove session data | For local saved-session data, run `anza diagnostics --delete-local-session NAME` using the exact name shown locally. For a hosted conversation, use its explicit delete action in the client or ask the operator for the current supported procedure; deletion does not erase provider-side copies or an email already sent. |
| A vendor tool is missing or login is not verified | Run `anza doctor`, then follow the vendor's own install/login steps. `anza run codex --provider subscription --version VERSION` or `anza run claude --provider subscription --version VERSION` launches the selected agent when supported; choose `openrouter` when appropriate. A successful local install does not prove model access; paid live checks require separate confirmation. |
| A command or option is unknown | Run `anza help`. Use `anza version` when reporting the issue and include only the redacted output from `anza diagnostics --preview` after review. |

Do not interpret “ready” as customer acceptance, commercial success, or completion of all project work. It means only the checks reported for that specific machine and plan.

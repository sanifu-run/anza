# Vendor compatibility and configuration

Research retrieved 2026-09-28. This is documentary evidence only; no vendor software was installed or exercised on target platforms. Every compatibility cell below remains `documented_only`. “Manual” describes the proposed Anza execution method where the vendor has not documented a reproducible, integrity-checked installation flow for that target; it does not mean a native test passed.

## Supported method evidence

| Vendor / path | Documented platform and prerequisites | Method, privilege, updates and pinning | Integrity and detection | Evidence |
|---|---|---|---|---|
| Claude Code via npm | Anthropic lists macOS 10.15+, Ubuntu 20.04+/Debian 10+, 4 GB RAM, Node.js 18+. Native Windows requires Git for Windows/Git Bash; WSL 1/2 also documented. Anthropic country availability applies. | `npm install -g @anthropic-ai/claude-code`; do not use `sudo`. Requires a user-writable npm prefix or administrator-provisioned npm environment. Startup/periodic background auto-update is on by default. Disable with `claude config set autoUpdates false --global` or `DISABLE_AUTOUPDATER=1`. npm permits exact version selection, e.g. `@2.1.276`; turn off Claude's updater to preserve that pin. | npm registry package integrity is not an Anza-verified vendor release digest/signature in the sources reviewed. Treat install as `manual` until an approved integrity check is specified. After install, run read-only `claude --version` and `claude doctor`; neither proves package provenance. | `claude-npm`; source says current package `2.1.276` at retrieval. |
| Claude Code native installer | Anthropic labels native binary install alpha; macOS/Linux and Windows through WSL. Exact tested distro/architecture list is not stated. | Anthropic's shell/PowerShell bootstrap installs per-user; no `sudo` documented. Auto-update is on by default and can be disabled with the same Claude settings/environment controls. The documentation reviewed gives no supported exact-version install/pin syntax. | No published digest or signature verification method was found in the cited instructions. Do not automate this method; `manual`. Detect with `claude --version` and `claude doctor`. | `claude-install`, `claude-setup`. |
| Claude subscription | Same installed-platform prerequisites. | Run `claude`, then complete the vendor sign-in flow for an eligible Claude plan or console/API account. This is vendor subscription/API authentication and is separate from an OpenRouter key. Preserve existing cached login unless the participant explicitly chooses to switch. No authentication or logout was performed here. | Not applicable to installation integrity. Confirm selected login through Claude's `/status`; this is read-only. | `claude-setup`, `claude-cli`. |
| Claude Code via OpenRouter | Claude Code can use OpenRouter's Anthropic-compatible Messages endpoint; OpenRouter says its Claude Code integration is guaranteed only with Anthropic first-party provider routing. This is configuration support, not a promise that every model supports every Claude Code feature. | Participant supplies an OpenRouter key. Inject at launch: `ANTHROPIC_BASE_URL=https://openrouter.ai/api`, `ANTHROPIC_AUTH_TOKEN=$OPENROUTER_API_KEY`, and explicitly empty `ANTHROPIC_API_KEY`. Keep the key in the credential store/process environment; do not write it to project files. Select a full OpenRouter model slug using Claude's supported model selection. Existing subscription login and OpenRouter credentials are separate modes. Existing cached Anthropic login may conflict; explain the vendor-documented manual `/logout` action but never invoke it automatically. Requests consume OpenRouter balance. | No install. Read-only `/status` can show auth source and endpoint; no request was made. Tool use follows the Anthropic Messages-compatible API, but model-specific tools, context, and features still require live qualification. | `openrouter-claude`, `openrouter-messages`. |
| Codex CLI via npm | Official Codex repository install instructions list macOS 12+, Ubuntu 20.04+/Debian 10+, 4 GB RAM (8 GB recommended), and Windows 11 via WSL2. This does not qualify native Windows. | `npm install -g @openai/codex`; use a user-writable npm prefix. The CLI checks for updates at startup and surfaces update prompts by default. `check_for_update_on_startup = false` disables that check/prompt; it is not an installer pin. npm exact-version selection (for example `@0.157.1`) is available, but avoid presenting it as an auto-update lock unless update behavior is disabled. | npm registry integrity metadata is not an Anza-verified vendor release digest/signature in the cited install instructions. Keep the automated installer `manual` until Anza verifies provenance. Detect read-only with `codex --version` and `codex login status`. | `codex-install`, `codex-release`, `codex-config`. Latest stable release at retrieval: `0.157.1`; GitHub also lists newer prereleases, which are not selected. |
| Codex subscription | Same documented CLI platform prerequisites. | Run `codex` and choose ChatGPT sign-in, or use `codex login`; this is ChatGPT account/plan access and distinct from OpenAI API billing. OpenAI API-key auth is also an available but separate billing path, not a ChatGPT subscription. No sign-in was performed here. | No install. `codex login status` is read-only. | `codex-auth`. |
| Codex CLI via OpenRouter | OpenRouter documents Codex CLI custom providers using its Responses endpoint; supported model availability and tool behavior are model/provider-specific. | Select with user-level `~/.codex/config.toml`; Codex ignores custom provider definitions in project-local config. Supply key via credential store/launch environment; OpenRouter usage is billed to its account. Keep ChatGPT subscription auth as a separate mode and preserve it. | No install. `codex --version` and `codex login status` inspect the binary/login, but do not prove provider connectivity. OpenRouter documents `wire_api = "responses"` and the `/api/v1/responses` route. Tool-call behavior and supported models remain untested here. | `codex-openrouter`, `openrouter-responses`, `codex-config`. |

Minimal Codex OpenRouter config, based on OpenRouter's integration guide:

```toml
model = "<provider>/<model-slug>"
model_provider = "openrouter"

[model_providers.openrouter]
name = "OpenRouter"
base_url = "https://openrouter.ai/api/v1"
wire_api = "responses"

[model_providers.openrouter.auth]
command = "sh"
args = ["-c", "echo $OPENROUTER_API_KEY"]
```

On Windows PowerShell, use the OpenRouter-documented PowerShell auth command. The key is a placeholder supplied to the process; it is never persisted by this example.

The Codex model provider and provider table belong in user-level config. Anza should not permanently change global provider selection to OpenRouter: resolve the selected mode in a child launch environment/config profile and preserve existing settings. Neither example relaxes approvals or permission mode.

## Platform matrix

Each cell has an explicit evidence level and source/method ID. Native/VM testing is deferred to the platform qualification task. The pilot target list is macOS arm64/amd64, native Windows amd64, Ubuntu 22.04/24.04 amd64, and Debian 12 amd64/arm64.

| Platform | Claude Code | Codex CLI | Qualification note |
|---|---|---|---|
| macOS arm64 | `documented_only` — npm method `claude-npm`; native installer also documented (`claude-install`). | `documented_only` — `codex-install`; current release assets include Apple arm64. | Target; native execution untested. |
| macOS amd64 | `documented_only` — `claude-npm`; native installer also documented. | `documented_only` — `codex-install`; current release assets include Apple x86_64. | Target; native execution untested. |
| Windows 11 amd64, native | `documented_only` — `claude-npm` with Git for Windows/Git Bash, or native installer. | `documented_only` — `codex-install` says Windows 11 via WSL2 only; native CLI is therefore `unsupported` by this evidence and must stay `manual`/unavailable pending official support and a native test. | Claude prerequisite is Windows shell setup. Codex's bundled Windows artifacts do not override its documented WSL-only system requirement. |
| Windows 11 arm64, native | `documented_only`; manual/unsupported for this pilot because the source does not claim ARM64 Windows. | `documented_only`; unsupported by the cited WSL2-only platform requirement for this pilot. | No native ARM qualification. |
| WSL 1 | `documented_only` — Anthropic documents WSL 1; manual pending qualification. | `documented_only`; unsupported by the cited Codex WSL2 requirement. | Do not conflate with native Windows. |
| WSL 2, Windows amd64 host | `documented_only` — Anthropic documents WSL 2. | `documented_only` — Codex repository documents Windows 11 via WSL2. | Manual setup; no Windows/WSL execution observed. |
| Ubuntu 22.04 amd64 | `documented_only` — `claude-npm`. | `documented_only` — `codex-install`. | Target; distro-specific package dependencies and launches untested. |
| Ubuntu 24.04 amd64 | `documented_only` — falls within the Anthropic Ubuntu 20.04+ statement. | `documented_only` — falls within the Codex Ubuntu 20.04+ statement. | Target; untested. |
| Debian 12 amd64 | `documented_only` — `claude-npm` Debian 10+. | `documented_only` — `codex-install` Debian 10+. | Target; untested. |
| Debian 12 arm64 | `documented_only` — general Debian minimum stated, architecture-specific matrix not stated; manual pending native qualification. | `documented_only` — general Debian minimum stated, architecture-specific support must be confirmed against release assets and native qualification; manual. | Target; untested. |
| Other glibc Linux x86_64/arm64 | `documented_only`; distro/architecture not enumerated; manual pending qualification. | `documented_only`; distro/architecture not enumerated in the system table; manual pending asset/native qualification. | Do not infer compatibility from Ubuntu/Debian minimums. |
| Linux musl / Alpine | `documented_only`; unsupported for automation because vendor docs do not list musl/Alpine. | `documented_only`; release assets include musl artifacts, but Codex system requirements do not qualify Alpine; manual only pending testing. | An artifact existing is not a supported platform claim. |

“Unsupported” here means Anza must not automatically install/launch on the basis of the cited vendor documentation. It does not assert that an undocumented combination cannot happen to run.

## Auth, model and tool-call boundaries

- Claude subscription sign-in is done inside Claude Code. OpenRouter uses a separate OpenRouter API key and Claude-compatible endpoint. A ChatGPT subscription, an OpenAI API key and an OpenRouter key are three distinct credentials and billing paths.
- Codex ChatGPT sign-in is separate from API-key authentication. OpenRouter uses its own key, custom provider name and Responses route. The examples intentionally keep `model` as a placeholder because model catalog and tool support change.
- OpenRouter documents its Responses endpoint and Claude Messages-compatible endpoint. Those protocol docs do not qualify every model's coding-agent tool calls, extended thinking, context, streaming, or error behavior. No inference of universal agent compatibility is made.
- Do not save secrets in `.claude/settings.json`, `.codex/config.toml`, project `.env`, receipts or logs. Never perform automatic logout, enable blanket trust, disable approvals, or bypass permission prompts while changing providers.

## Integrity and installation policy

Where the official method does not publish a digest/signature and verification instruction that Anza can check, classify that path as `manual`, even if its transport is HTTPS or its package manager has its own registry integrity mechanism. The current sources reviewed do not establish a vendor-signed checksum workflow for Claude's npm/native installation, nor an Anza-verifiable checksum workflow for Codex's npm CLI. Do not invent download URLs or checksums. Exact npm package versions and Codex release tags are pin inputs, not proof of artifact integrity. Read-only post-install checks are `claude --version`, `claude doctor`, and `codex --version`; these detect version/install health only. Automated installation remains gated on an explicit integrity-verification design plus platform qualification.

## Sources

Exact source URLs, retrieval date, method, version statements, evidence classes and limitations are recorded in [`vendor-methods.json`](research/vendor-methods.json).

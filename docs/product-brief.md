# Anza product brief

Date: 2026 09 28. Status: founder intent plus explicit implementation defaults.

## Founder decisions

- Go CLI at sanifu-run/anza, installable with one terminal command appropriate to the operating system. No Go compiler required on participant machines.
- macOS, Windows and Linux in the first release; beginner and developer profiles.
- Install both Codex and Claude Code by default. Development access is participant-owned ChatGPT/Claude subscriptions or OpenRouter keys; credentials can differ between agents.
- Curate content from the founder's portable workflows and ECC. Do not copy the personal machine configuration wholesale.
- Participants bring their own real project. A paying customer is optional. Support recommendations for any project domain, including existing codebases.
- Anza conducts an LLM-assisted interview and tailors setup. A limited Sanifu-hosted interview lets participants begin before agent login or API-key acquisition. Founder subsequently directed reuse of the existing chat backend/interview instead of a separate service.
- Deliver complete workshop readiness: prerequisites, account guidance, relevant skills, configuration and a meaningful first check for the participant's project.
- This request is planning, including prescriptive tasks executable in parallel by GPT-6-Luna. It does not request running the implementation or authorize unbounded service spending.

## Defaults chosen for implementation, not additional founder decisions

An interactive, accessible line-oriented terminal wizard is the primary interface. It explains one question at a time, permits skip/back/resume, and shows what leaves the machine. Beginner mode explains terminology; developer mode shows versions, diffs and overrides. Both receive the same preservation rules.

The model proposes catalog IDs and explanations. A deterministic local planner resolves those IDs into reviewed recipes. Participant approval binds to a digest of the exact local plan. Unknown stacks receive useful recommendations and manual steps, never invented support claims or generated shell execution.

Install project skills in project-local managed directories. Do not replace a participant's entire global instruction file, skill directory or agent configuration. Optional GitHub CLI/editor/browser/MCP additions require inclusion in the reviewed plan; no default marketplace sweep or imported personal hooks.

Reuse the existing chat backend with an additive setup-purpose interview. It shares the model transport, token/session lifecycle, DynamoDB and deployment. Optional import accepts a learner-selected approved brief file or reviewed plain text; no automatic browser transcript retrieval or recovery-token harvesting. The Go CLI owns local planning and execution.

Use the existing conversation recovery-token protocol with new persistent setup usage limits. No new invitation/account system is needed. New setup mode is disabled until its catalog/limits/privacy policy and shared-route regressions are qualified. A hosted browser, community app, IDE, agent orchestrator and deployment service are outside this release.

## Success and honest status

- On each qualified platform, a fresh participant can bootstrap Anza, complete the interview, review setup and reach accurate per-tool readiness.
- Existing configuration and user edits survive repeat setup, interrupted setup, update and uninstall.
- Paid agent checks occur only after a separate participant confirmation; local installation success is never reported as successful model authentication.
- Project recommendation coverage is broad; automatic installation coverage is an explicit tested matrix. iOS builds require macOS and vendor SDKs; no promise to install Xcode or bypass store/license prompts on another OS.
- Pilot measurement: record opt-in setup duration, manual interventions and user-reported completion without collecting code, customer data, transcripts or secrets. Target <=15 minutes of active participant setup excluding downloads, OS reboots, account creation and vendor login; measure, do not advertise until observed.

## Launch decisions still needed

At the release gate: incremental setup/session caps under the existing provider route and budget, shared-backend rollout scope/operator, public installer URL and existing API origin, accurate shared retention/mail notice, Anza license/redistribution rights and supported version matrix. Review existing authorization before requesting additional scope; do not ask the founder to select a duplicate service host. Preparation and local fixture work can proceed before these decisions. Paid calls, production deployment and publication require applicable scoped authorization.

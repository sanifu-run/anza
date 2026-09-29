# ADR 001: Project-driven bootstrap with a hosted interview

## Status

Accepted founder direction; implementation defaults identified separately.

## Date

2026-09-28

## Context

Participants bring projects with different domains and experience levels. Requiring agent credentials before the interview would make setup circular.

## Decision

Use a Go CLI on macOS/Windows/Linux, both agents by default, two guidance profiles, a limited Sanifu-hosted LLM interview, participant-owned development access and curated skills. Support optional learner-reviewed brief import, while Anza independently interviews for missing setup facts. Paying customers are optional. Founder subsequently directed reuse of the existing chat backend/interview. ADR 006 governs shared model routing, session/storage infrastructure and deployment; the CLI retains local execution responsibility.

## Consequences

Beginners can reach recommendations before agent login. Hosting introduces operating cost, privacy and admission requirements; host/model/budget and publication scopes remain launch decisions. Project breadth does not imply universal automatic installation.

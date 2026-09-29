# ADR 003: Separate participant credentials and portable content

## Status

Founder-approved access/content direction with selected implementation defaults.

## Date

2026-09-28

## Context

Founder setup includes shared skills, personal hooks, gateways and tools. Blind copying would import assumptions and credentials. The two agents use different provider configuration surfaces.

## Decision

Support subscriptions and OpenRouter independently per agent. Hosted interview secrets stay in the existing chat backend and use its current model router/runtime secret loading. Participant OpenRouter keys use OS credential storage or explicit session-only input; native subscription tokens remain with their vendor. anza run assembles child-only environments. Ship small namespaced project-local skill copies from one versioned content source, preserving existing roots/configuration. Attribute audited ECC derivatives and write original portable replacements where rights or dependencies are unclear.

## Consequences

Agent adapters must be version-qualified independently. Direct agent launch may differ from the Anza OpenRouter launch path; explain it. No assumption that 140 disabled local path entries or installed skill directories represent effective participant capabilities. Plugins and private MCP dependencies are not copied automatically.

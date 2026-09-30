# Anza participant privacy notice (draft for release review)

This is a draft of the current implementation's data flow, not newly published service terms. Anza's hosted interview is designed to use the existing Sanifu chat service. Setup capability must be enabled and the matching release deployed before the flow is available.

## What is sent and stored

Before a hosted request, Anza presents reviewed machine facts and brief/context for participant approval. The protocol excludes names, contacts, raw paths, usernames, hostnames, hardware serials, raw project files, browser recovery tokens, and participant agent credentials. Do not enter secrets, private source, customer records, or confidential material in interview answers or imported briefs. The model provider receives submitted messages and setup context needed to answer.

The existing chat backend stores setup conversations in its AWS DynamoDB transcript table. Its configured TTL is 30 days after the last update. Expired records are hidden at the expiry boundary; DynamoDB removes them asynchronously. Point-in-time recovery is disabled for the described table. This duration applies only to the AWS transcript copy.

The selected `gpt-6-luna` route uses Experiential Labs. Production prompt capture is enabled without a fixed expiry. Provider retention is separate from AWS retention and is outside deletion through the website/API. Other explicitly selected model routes may have different provider policies and must be separately reviewed before use.

Setup-purpose conversations are excluded from scheduled owner transcript email by the chat mailer. Anza setup consent does not grant consent to email an owner a transcript. An email copy already sent for a different conversation remains an independent inbox copy.

## Access and deletion

The conversation recovery-token holder can delete an active hosted conversation through the shared conversation deletion API. Deletion removes transcript content and calls the optional setup coordination cleanup hook. Minimal non-content anti-replay/budget metadata can remain until its separately configured tombstone TTL, which must be confirmed during deployment review. Transcript TTL expiry is asynchronous and does not invoke that cleanup hook. Deletion does not erase provider-captured data, an email already sent, or a booking in Cal.com. The data policy makes no separate setup backup-copy claim.

Authenticated service owners can explicitly inspect setup records through the admin API; default owner lists omit them. These controls are verified in Chat source and fixture tests, not by a new live service test for this document.

## Release review still required

Before enabling or advertising hosted setup, confirm the live capability, model/provider selection, configured AWS TTL, coordination tombstone TTL, email exclusion and deployed code revision. Confirm the participant-facing notice and support contact in the actual client. This draft does not grant permission for additional provider spend, deployment, owner email, or publication.

Evidence: Chat `docs/anza-data-policy.md`, Chat T3.8 implementation evidence, Anza ADR 006 and `docs/research/chat-reuse.md`. Policy review date: 2026-09-29.

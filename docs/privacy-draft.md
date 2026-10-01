# Anza participant privacy notice (draft for release review)

This draft describes the hosted Anza interview implemented through the existing Sanifu Chat service. It is not a final service agreement. The facts below come from Chat source, service policy, and task evidence; verify them against the deployed revision before enabling or advertising the feature.

## What is sent and stored

Before a hosted request, Anza presents reviewed machine facts and brief/context for participant approval. The protocol excludes names, contacts, raw paths, usernames, hostnames, hardware serials, raw project files, browser recovery tokens, and participant agent credentials. Do not enter secrets, private source, customer records, or confidential material in interview answers or imported briefs. The selected model provider receives submitted messages and setup context needed to answer.

The AWS DynamoDB transcript table is configured for a 30-day TTL after the record's last update. Expired records are hidden at the expiry boundary; DynamoDB physically removes them asynchronously. Point-in-time recovery is disabled for the described transcript table. Service logs have a separate 14-day retention period. Those durations describe AWS copies only.

The hosted shared Chat route currently selects Experiential Labs `gpt-6-luna`. Production prompt capture is enabled and has no fixed expiry. Provider retention is separate from AWS retention and cannot be removed through the Anza or Chat deletion flow. A participant's ChatGPT or Claude subscription login and participant-owned OpenRouter credentials are separate arrangements used by participant-selected agents; they do not configure the hosted interview route.

Setup-purpose conversations are excluded from scheduled owner transcript email. Anza setup consent does not grant consent to email an owner a transcript. An owner email copy already sent for another conversation is an independent inbox copy and is not covered by AWS transcript expiry. Setup conversations are excluded from that scheduled email, but deployment review must confirm the exclusion is active in the deployed mailer.

## Access and deletion

The conversation recovery-token holder can delete an active hosted conversation through the shared conversation deletion API. Deletion removes transcript content and calls the optional setup coordination cleanup hook. Minimal non-content anti-replay and budget metadata can remain until its separately configured tombstone TTL; confirm that TTL during deployment review. Transcript TTL expiry is asynchronous and does not invoke the cleanup hook. Deletion does not erase provider-captured data, an email already sent, or a booking in Cal.com. The current data policy makes no separate setup backup-copy claim.

Authenticated service owners can explicitly inspect setup records through the admin API; default owner lists omit them. These controls are verified in Chat source and fixture tests, not by a live service test for this document.

## Release review still required

Before enabling or advertising hosted setup, verify the live capability, deployed revision, model/provider route and prompt-capture terms, AWS transcript TTL, 14-day log policy, coordination tombstone TTL, setup mail exclusion, participant-facing notice, and support contact. This draft does not grant permission for additional provider spend, deployment, owner email, or publication.

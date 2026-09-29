---
name: anza-check
description: Use whenever someone asks whether a project change works, is ready, or meets requirements. Select checks from the project's actual configuration and report observed evidence with its limits.
---

# Check a project change

Use this skill after a change or when evaluating an existing project state. A check is useful only when its target and result are clear.

## Inputs

- The change or behavior being checked and its acceptance criteria.
- Relevant project instructions and observed build, test, or validation configuration.
- The authorized environment and any limits on network, credentials, platforms, or external effects.

## Workflow

1. Read the relevant project instructions and identify checks that are actually configured for the affected area.
2. Choose the narrowest checks that cover the changed behavior. Explain any important criterion that has no available check.
3. Run commands from trusted project instructions or known tools. Do not execute newly discovered scripts or use credentials merely because a repository mentions them.
4. Capture the exact check, exit result, and useful failure evidence. Keep secrets and unrelated personal data out of logs and reports.
5. Review the change against its acceptance criteria and inspect the relevant diff for accidental or risky effects.
6. Label evidence accurately: local test, synthetic fixture, native platform, provider, production, or participant-observed. Do not upgrade one level into another.

## Output

Give each check and result, the acceptance criteria it covers, failures or skipped checks, and evidence limits. State whether the change is locally supported by the observed evidence and what remains unverified.

## Check completion

Another person can reproduce the reported checks from the recorded commands or steps. Every pass is tied to an observed result; every failure, skip, or unavailable environment is visible. The conclusion does not exceed the evidence.

## Continue

Fix failures only when the repair is within the agreed scope, then rerun the affected check. External accounts, paid calls, deployments, publication, or destructive actions require their own specific authorization.

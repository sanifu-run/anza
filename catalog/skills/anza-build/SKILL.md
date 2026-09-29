---
name: anza-build
description: Use when asked to implement an agreed project change, fix a defect, or make a bounded improvement. Apply the smallest useful change, preserve surrounding work, and leave reviewable evidence.
---

# Build a bounded change

Use this skill when the requested outcome and boundaries are clear enough to implement. If they are not, clarify the scope first. Work in the participant's project only when access and the task authorize it.

## Inputs

- The agreed outcome, acceptance check, and relevant plan item.
- Project instructions and the files needed to understand the change.
- Current change state, available checks, and any explicit permission limits.

## Workflow

1. Read relevant local instructions and inspect the smallest code or content area needed. Notice existing edits before changing files.
2. Make one useful, coherent slice that satisfies the acceptance check. Match the project's conventions and keep unrelated formatting or cleanup out of the diff.
3. Preserve neighboring and concurrent work. Do not overwrite changes you did not make; resolve ownership or merge conflicts before proceeding.
4. Keep effects within the agreed scope. Do not execute untrusted project scripts, access secrets, install software, publish, spend money, or change shared services unless that specific action is authorized.
5. Run relevant, bounded checks supported by the project instructions. If a check is unavailable or unsafe, explain the gap instead of inventing a pass.
6. Review the resulting changes for accidental files, unrelated edits, sensitive data, and unmet acceptance criteria.

## Output

Report the behavior changed, the files or areas affected, checks run and their results, remaining limitations, and the next step if work is incomplete. Include a reviewable diff or artifact when the task calls for one.

## Check completion

The change meets its stated acceptance check, the relevant local checks have observed results, and the final diff stays within the agreed scope. A passing local check does not establish native-platform, provider, production, or user qualification unless that evidence was actually collected.

## Continue

If blocked, preserve the partial work and identify the missing input, interface, or permission precisely. Continue any independent item that remains within scope; do not report planned behavior as implemented.

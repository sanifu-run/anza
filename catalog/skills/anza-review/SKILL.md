---
name: anza-review
description: Use when asked to review a project change, pull request, patch, or bounded set of files. Look for concrete defects and scope risks, and report evidence that the author can act on.
---

# Review a project change

Review the requested change against its intended behavior. A review is an independent assessment, not a substitute for checks the project requires.

## Inputs

- The diff or artifact to review, its intended outcome, and acceptance criteria.
- Relevant project instructions and nearby implementation context.
- Any explicit review focus, such as correctness, compatibility, privacy, or preservation.

## Workflow

1. Establish the review boundary and inspect the actual changed material. If there is no diff, clarify which version or files to assess.
2. Trace important behavior from input through output and side effects. Check error paths, boundary values, compatibility, and whether user data or existing work can be lost or exposed.
3. Compare changed behavior with the task and local contract. Check for accidental scope expansion, unsupported claims, hidden external actions, and missing evidence.
4. Support each finding with a file and location plus the concrete trigger and likely effect. Prioritize defects by impact and likelihood.
5. Separate actionable findings from questions, risks, and checks that were not performed. If no issue is found, say what scope was reviewed and which validation remains open.

## Output

List findings first, ordered by severity, with location and a short explanation. Then give the reviewed scope, relevant strengths or non-blocking risks, and checks or areas not covered. Avoid unsupported praise or a blanket approval based on a narrow review.

## Check completion

Each finding is reproducible from the cited change and explains why it matters. The review covers the requested boundary and states its limits. A reader can distinguish a defect from a suggestion or unresolved question.

## Continue

If requested, review the repair against the original finding and check for regressions in the affected area. Do not modify the change during a review unless the participant also asks for implementation.

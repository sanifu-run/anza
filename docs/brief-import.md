# Learner brief import

The setup flow accepts only a learner-selected `.json`, `.txt`, or `.md` file. It reads at most 32 KiB and never scans a project, browser profile, transcript store, local storage, or recovery-token store. Import is local and read-only: it does not upload, mutate the selected file, make a network request, or infer a learner decision.

JSON uses the shared strict `ProjectBrief` schema, including duplicate-key and unknown-field rejection. Credential, contact, and token fields are not part of that schema. The user sees a structured preview before approving it. Approval is explicit; until then no upload context is returned. Approved typed data carries `source.kind=learner_export` and `source.reviewed=true`.

Text is preserved as prose, displayed with an untrusted-context warning, and remains labeled untrusted in the resulting setup context. It is not converted into structured facts; the setup interview must confirm missing facts. Invalid UTF-8 and whitespace-only text are rejected.

File content is bounded at 32 KiB; serialized approved context is bounded at 30 KiB. Read errors and schema errors contain no imported values. Review and upload are separate application steps; this package only returns context after explicit review and performs no service call itself.

{{- define "followup-sink" -}}
<followup-sink>

**Follow-up sink.** Every follow-up recorded here goes to the resolved issue tracker, or to the local ledger when that tracker cannot take it. The caller supplies the title, a body naming the origin, file, and finding, a severity (`risk`, `nit`, or `question`), and a kind (`finding` or `plan`).

1. **Tracker.** {{ template "resolved-provider" "issue-tracker" }}
2. **Redact and preview.** Redact the body per the **Privacy** rules below. When redaction would drop something load-bearing, say so in the body instead of asking. Show the tracker, the target project, the rendered title and body, and the intended labels in the same message that offers the caller's choice. That choice is the go-ahead the privacy rules ask for; ask no other question.
3. **Hosted tracker.** When the tracker is not `filesystem`, run its `auth-check` and resolve its `project-identifier`. Only when the check passes and the project resolves to exactly one team, project, or board, run `label-list` and keep only the severity and kind labels it reports as existing; never create a label. When `label-list` cannot run, apply no label, write the severity and kind into the body, and count both labels as skipped. Then run `issue-create` with the title, body, and kept labels.
4. **Filesystem.** Read `references/issue-tracking/filesystem.md` in the `atomic-git-discipline` skill and run its `issue-create` when any of these holds:
    - the tracker is `filesystem`;
    - `auth-check` fails;
    - the project does not resolve to exactly one team, project, or board, as when `issue-tracker-project` is unset and several are reachable;
    - `issue-create` fails.

    Except for `filesystem` itself, report in one line why the tracker was not used.
5. **Result.** Print one line per follow-up, naming the tracker used: `follow-up: <tracker> <issue URL, key, or ledger entry id> (skipped labels: <list or none>)`.

Keep no record of which tracker was used beyond that line; the next run resolves again.

**Privacy.**

{{ template "report-issue-privacy" . }}

</followup-sink>
{{- end -}}

#!/usr/bin/env bash
# Deterministic staleness classifier for the repo-local /triage-issues command.
#
# Emits a JSON array, one object per open issue (or per issue number passed as
# an argument), describing the staleness state. No judgment, no mutation — this
# is the "code" half of triage; label suggestion and comment drafting (the
# "model" half) live in .claude/commands/triage-issues.md.
#
# Per-issue object: { number, title, reporter, labels, idleDays, action }
# action ∈ nudge | close | unstale | wait-nudge | wait-close | none
#   nudge       maintainer replied last, reporter silent >=14d, not yet nudged
#   close       already nudged (stale label / marker), reporter silent >=14d more
#   unstale     reporter re-engaged but the stale label lingers
#   wait-nudge  on the nudge track but under 14d idle
#   wait-close  on the close track but under 14d since the nudge
#   none        ball is with the maintainers; nothing to do
#
# Usage: scripts/triage-scan.sh [issue-number ...]   (no args = all open issues)
set -euo pipefail

command -v gh >/dev/null 2>&1 || { echo "triage-scan: gh not found (brew install gh)" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "triage-scan: jq not found (brew install jq)" >&2; exit 1; }

# Collect issue numbers: explicit args, else every open issue. (bash 3.2 — no mapfile.)
numbers=()
if [ "$#" -gt 0 ]; then
    numbers=("$@")
else
    while IFS= read -r n; do
        [ -n "$n" ] && numbers+=("$n")
    done < <(gh issue list --state open --limit 200 --json number | jq -r '.[].number')
fi

# The classifier. Single-quoted so bash leaves the jq $vars literal.
# shellcheck disable=SC2016  # the $vars are jq variables, not shell — keep them unexpanded
classify='
  .author.login as $r
  | (.labels // [] | map(.name)) as $labels
  | (.comments // []) as $cs
  | (if ($cs|length) > 0 then $cs[-1] else null end) as $l
  | now as $n
  | ($l != null
       and ($l.author.login != $r)
       and (["OWNER","MEMBER","COLLABORATOR"] | index($l.authorAssociation) != null)) as $m
  | ($labels | index("stale") != null) as $sl
  | ($m and ($l.body | test("atomic-triage:nudge"))) as $nudged
  | (if $l != null
       then (($n - ($l.createdAt|fromdateiso8601))/86400 | floor)
       else (($n - (.createdAt|fromdateiso8601))/86400 | floor) end) as $i
  | {number, title, reporter:$r, labels:$labels, idleDays:$i,
     action:(
        if ($m|not) then (if $sl then "unstale" else "none" end)
        elif ($sl or $nudged) then (if $i>=14 then "close" else "wait-close" end)
        else (if $i>=14 then "nudge" else "wait-nudge" end)
     end)}'

{
    for N in "${numbers[@]}"; do
        gh issue view "$N" --json number,title,author,body,labels,createdAt,comments \
            | jq -c "$classify"
    done
} | jq -s '.'

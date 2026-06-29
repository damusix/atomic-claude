// Content for the "How it flows" session player on the concepts page. Same data
// shape as session-script.ts (see that file for the kind/tone model); this one
// walks a single real lifecycle end to end — adding a Stripe webhook endpoint to
// a NestJS app — across every command in the loop.

import type { SessionSlide, OutputLine } from './session-script'

const gap: OutputLine = { kind: 'gap' }

export const FLOW: SessionSlide[] = [
    {
        id: 'signals',
        label: 'signals',
        command: '/refresh-signals',
        output: [
            { kind: 'say', text: 'Scanning the repo to build its map.' },
            { kind: 'tool', text: 'Bash(atomic signals scan)' },
            { kind: 'out', text: 'NestJS · Prisma · Jest · docs/' },
            gap,
            { kind: 'say', text: 'Wrote signals.md — Claude reads it', tone: 'ok' },
            { kind: 'say', text: 'before your code, every session.', tone: 'ok' },
        ],
    },
    {
        id: 'evidence',
        label: 'evidence',
        command: '/gather-evidence does the Stripe SDK verify webhooks?',
        output: [
            { kind: 'say', text: 'Falsifiable claim, then the docs.' },
            { kind: 'tool', text: 'context7(stripe-node, webhooks)' },
            { kind: 'out', text: 'Stripe.webhooks.constructEvent' },
            { kind: 'cont', text: 'sig + tolerance + replay guard' },
            gap,
            { kind: 'say', text: 'VERDICT: SUPPORTED', tone: 'ok' },
            { kind: 'say', text: 'Hunch holds — no hand-rolled HMAC.' },
        ],
    },
    {
        id: 'plan',
        label: 'plan',
        command: '/atomic-plan POST /api/webhooks: verify + queue',
        output: [
            { kind: 'say', text: 'Non-trivial. Design doc, then spec.' },
            { kind: 'tool', text: 'Write(docs/spec/stripe-webhooks.md)' },
            { kind: 'out', text: 'controller · service · DTO' },
            { kind: 'cont', text: 'signature check · queue · retry' },
            gap,
            { kind: 'say', text: 'You cut retry from scope. Approved.', tone: 'ok' },
        ],
    },
    {
        id: 'implement',
        label: 'implement',
        command: '/subagent-implementation',
        output: [
            { kind: 'say', text: 'Each checkpoint: implementer writes' },
            { kind: 'say', text: 'test-first, reviewer gates.' },
            gap,
            { kind: 'tool', text: 'Task(atomic-implementer, cp 1/3)' },
            { kind: 'out', text: 'failing test → controller → pass' },
            { kind: 'tool', text: 'Task(atomic-reviewer, cp 1/3)' },
            { kind: 'out', text: 'APPROVED', tone: 'ok' },
            gap,
            { kind: 'say', text: 'cp 2/3, cp 3/3 — same loop.' },
            { kind: 'out', text: 'ledger: 1 fixed · 2 deferred', tone: 'muted' },
            gap,
            { kind: 'tool', text: 'Bash(npx jest)' },
            { kind: 'out', text: 'ok   37 passed', tone: 'ok' },
        ],
    },
    {
        id: 'report',
        label: 'report',
        command: '/session-report',
        output: [
            { kind: 'say', text: 'Capturing the why behind choices.' },
            { kind: 'out', text: 'constructEvent over hand HMAC' },
            { kind: 'out', text: 'retry skipped → follow-up' },
            { kind: 'out', text: 'reviewer flags recorded' },
            gap,
            { kind: 'say', text: 'Saved. Ship verbs read this.', tone: 'ok' },
        ],
    },
    {
        id: 'ship',
        label: 'ship',
        command: '/commit pr',
        output: [
            { kind: 'say', text: 'Refresh signals, check docs, open PR.' },
            { kind: 'tool', text: 'Bash(atomic signals scan)' },
            { kind: 'out', text: '+ WebhooksController in the map' },
            { kind: 'say', text: 'New endpoint → API docs need a line.' },
            { kind: 'out', text: 'you update docs/api.md' },
            { kind: 'tool', text: 'Bash(gh pr create)' },
            { kind: 'out', text: '→ PR #88 opened', tone: 'ok' },
        ],
    },
    {
        id: 'remind',
        label: 'remind',
        command: '/remind-me check the webhook PR by thursday',
        output: [
            { kind: 'say', text: 'Scheduling a reminder.' },
            { kind: 'out', text: 'thu · "check webhook PR review"' },
            gap,
            { kind: 'out', text: '— two days later —', tone: 'muted' },
            { kind: 'say', text: 'Fires at your next session start:' },
            { kind: 'out', text: '⏰ check if the webhook PR got reviewed', tone: 'ok' },
        ],
    },
    {
        id: 'follow-up',
        label: 'follow-up',
        command: '/follow-up review',
        output: [
            { kind: 'say', text: 'Reviewing deferred work.' },
            { kind: 'out', text: 'retry logic — deferred in planning' },
            { kind: 'say', text: 'Promote it to a GitHub issue?' },
            { kind: 'tool', text: 'Bash(gh issue create)' },
            { kind: 'out', text: '→ #89  add webhook retry', tone: 'ok' },
        ],
    },
]

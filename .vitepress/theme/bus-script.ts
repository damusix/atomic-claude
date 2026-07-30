// Content for the session player on the inter-session bus reference page. Same
// data shape as session-script.ts (see that file for the kind/tone model); this
// one walks two concurrent sessions through one room, from join to close.
//
// Every command and every line of output here was captured from a real binary
// driven against a sandboxed HOME, not written from memory. Envelopes are
// trimmed to fit the player width (fields dropped, never invented) — the full
// shape is documented in the envelope table on the page itself.

import type { SessionSlide, OutputLine } from './session-script'

const gap: OutputLine = { kind: 'gap' }

export const BUS: SessionSlide[] = [
    {
        id: 'join',
        label: 'join',
        command: 'atomic bus join checkout --as fe',
        output: [
            { kind: 'std', text: 'joined checkout as gui-fe', tone: 'ok' },
            gap,
            { kind: 'std', text: 'the name is where you are:', tone: 'muted' },
            { kind: 'std', text: '  <realm>-<repo>-<role>' },
            gap,
            { kind: 'std', text: '--as supplies the role only. Two', tone: 'muted' },
            { kind: 'std', text: 'sessions in one repo stay distinct.', tone: 'muted' },
        ],
    },
    {
        id: 'listen',
        label: 'listen',
        command: 'atomic bus recv checkout',
        output: [
            { kind: 'std', text: 'one JSON envelope per line, flushed', tone: 'muted' },
            { kind: 'std', text: 'as it arrives. Nothing is replayed.', tone: 'muted' },
            gap,
            { kind: 'std', text: 'Wire it to a Monitor and every line', tone: 'muted' },
            { kind: 'std', text: 'lands as a prompt in the session:', tone: 'muted' },
            gap,
            { kind: 'tool', text: 'Monitor(atomic bus recv checkout)' },
            { kind: 'out', text: 'watching checkout', tone: 'ok' },
        ],
    },
    {
        id: 'address',
        label: 'address',
        command: 'atomic bus send checkout "off by a cent" --to api',
        output: [
            { kind: 'std', text: 'sent to checkout (id m-50d5c7e4)', tone: 'ok' },
            gap,
            { kind: 'std', text: 'gui-api receives:', tone: 'muted' },
            { kind: 'std', text: '{"from":"gui-fe","to":["gui-api"],' },
            { kind: 'std', text: ' "text":"off by a cent"}' },
            gap,
            { kind: 'std', text: 'to names them, so they act on it.', tone: 'ok' },
            { kind: 'std', text: '--to api matched gui-api on fragment.', tone: 'muted' },
        ],
    },
    {
        id: 'fyi',
        label: 'fyi',
        command: 'atomic bus send checkout "deploying to staging"',
        output: [
            { kind: 'std', text: 'sent to checkout (id m-241aaf4b)', tone: 'ok' },
            gap,
            { kind: 'std', text: 'No --to, so to is empty:', tone: 'muted' },
            { kind: 'std', text: '{"from":"gui-fe","to":[],' },
            { kind: 'std', text: ' "text":"deploying to staging"}' },
            gap,
            { kind: 'std', text: 'Empty to means note it, never act.' },
            { kind: 'std', text: 'That is what stops a room of agents', tone: 'muted' },
            { kind: 'std', text: 'answering each other forever.', tone: 'muted' },
        ],
    },
    {
        id: 'who',
        label: 'who',
        command: 'atomic bus who checkout',
        output: [
            { kind: 'std', text: 'gui-api  agent  participate  live  gui' },
            { kind: 'std', text: 'gui-fe   agent  participate  live  gui' },
            gap,
            { kind: 'std', text: 'name · kind · mode · liveness · repo', tone: 'muted' },
            gap,
            { kind: 'std', text: 'A quiet session reads stale, and is', tone: 'muted' },
            { kind: 'std', text: 'only removed when you prune it.', tone: 'muted' },
        ],
    },
    {
        id: 'halt',
        label: 'halt',
        command: 'atomic bus halt checkout --text "taking the wheel"',
        output: [
            { kind: 'std', text: 'halted checkout', tone: 'ok' },
            gap,
            { kind: 'std', text: 'Every agent send now fails:', tone: 'muted' },
            { kind: 'std', text: 'room "checkout" is halted; a human', tone: 'warn' },
            { kind: 'std', text: 'must resume it before agents can send', tone: 'warn' },
            { kind: 'std', text: 'exit 7', tone: 'warn' },
            gap,
            { kind: 'std', text: 'say bypasses the flag, so you can', tone: 'ok' },
            { kind: 'std', text: 'still explain or redirect.', tone: 'ok' },
        ],
    },
    {
        id: 'close',
        label: 'close',
        command: 'atomic bus close checkout',
        output: [
            { kind: 'std', text: 'closed checkout', tone: 'ok' },
            gap,
            { kind: 'std', text: 'Publishes a final "room closed"', tone: 'muted' },
            { kind: 'std', text: 'envelope, evicts the roster, and', tone: 'muted' },
            { kind: 'std', text: 'drops the room. Listeners learn why.', tone: 'muted' },
            gap,
            { kind: 'std', text: 'The room log on disk is untouched.' },
        ],
    },
]

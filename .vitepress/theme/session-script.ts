// Content for the home hero session player. Pure data — edit freely here without
// touching the player logic.
//
// Each slide is one section the viewer can jump to:
//   command  → typed character by character (what you type)
//   output   → revealed line by line (the session that follows)
//
// Two axes control how a line renders:
//   kind  → the chrome / prefix, mirroring Claude Code's transcript:
//             'say'  ⏺ Claude speaking (a sentence)
//             'tool' ⏺ a tool call, e.g. Bash(go test ./...) or Task(atomic-reviewer)
//             'out'  ⎿ a line of that tool's output
//             'cont'   continuation of an 'out' line (indented, no bracket)
//             'std'    raw shell stdout (no prefix) — for direct CLI commands
//             'gap'    a blank spacer line
//   tone  → a colour override on top of the kind's default:
//             'ok'   amber / success      'warn' orange / changes-requested
//             'muted' dimmer secondary
//
// Keep lines under ~50 chars so they don't clip; wrap longer prose with a
// 'cont' (under output) or a second 'say' line. Overflow scrolls — that's fine.

export type LineKind = 'say' | 'tool' | 'out' | 'cont' | 'std' | 'gap'
export type Tone = 'ok' | 'warn' | 'muted'

export interface OutputLine {
    text?: string
    kind?: LineKind
    tone?: Tone
}

export interface SessionSlide {
    id: string
    label: string // shown in the bottom navigation
    command: string // typed char-by-char (what you type)
    output: OutputLine[] // revealed line-by-line (the session)
}

const gap: OutputLine = { kind: 'gap' }

export const SESSION: SessionSlide[] = [
    {
        id: 'explore',
        label: 'explore',
        // a direct CLI command — output is plain shell stdout
        command: 'atomic code explore "snowflake sql"',
        output: [
            { kind: 'std', text: 'Exploring: snowflake sql', tone: 'muted' },
            gap,
            { kind: 'std', text: '12 symbols · 4 files · 0.3s', tone: 'muted' },
            gap,
            { kind: 'std', text: 'extraction/standalone/sql.go' },
            { kind: 'std', text: '  (*SQLExtractor).Extract       :913' },
            { kind: 'std', text: '  stageTokenPat  @stage @~ @%   :615' },
            { kind: 'std', text: 'indexer/embedded_sql_postpass.go' },
            { kind: 'std', text: '  embeddedSQLPostPass()         :166' },
            { kind: 'std', text: 'types/types.go' },
            { kind: 'std', text: '  NodeKind Stage Stream Task Model' },
            gap,
            { kind: 'std', text: 'SQL is a first-class node kind —', tone: 'ok' },
            { kind: 'std', text: 'dbt + Snowflake already graphed.', tone: 'ok' },
        ],
    },
    {
        id: 'evidence',
        label: 'evidence',
        command: '/gather-evidence does it pull SQL from app strings?',
        output: [
            { kind: 'say', text: 'Restating as a falsifiable claim,' },
            { kind: 'say', text: 'then scoping what would settle it.' },
            gap,
            { kind: 'tool', text: 'Grep(EmbeddedLiteralConfig, **/*.go)' },
            { kind: 'out', text: 'embedded_literals_config.go' },
            { kind: 'cont', text: '16 host languages registered' },
            { kind: 'tool', text: 'Read(embedded_sql_postpass.go)' },
            { kind: 'out', text: 'Go, Python, TS, TSX via post-pass' },
            { kind: 'cont', text: '→ 20 languages total' },
            gap,
            { kind: 'say', text: 'No counter-evidence after a' },
            { kind: 'say', text: 'directed search of the extractor.' },
            gap,
            { kind: 'say', text: 'VERDICT: SUPPORTED', tone: 'ok' },
            { kind: 'say', text: 'Proceed to /pressure-test.' },
        ],
    },
    {
        id: 'pressure',
        label: 'pressure',
        command: '/pressure-test @spec/snowflake-copy-lineage',
        output: [
            { kind: 'say', text: 'Three assumptions in the spec.' },
            { kind: 'say', text: "I'll try to break each one." },
            gap,
            { kind: 'say', text: '1. COPY INTO always targets a table.' },
            { kind: 'out', text: 'no — it can target a stage too.', tone: 'warn' },
            { kind: 'cont', text: '→ table only; stage→stage flagged.' },
            { kind: 'say', text: '2. FILE FORMAT is always a named ref.' },
            { kind: 'out', text: 'no — it can be inline.', tone: 'warn' },
            { kind: 'cont', text: '→ named resolves; inline no-ops.' },
            { kind: 'say', text: '3. lineage holds through SELECT *.' },
            { kind: 'out', text: 'out of scope. flag, don’t infer.' },
            gap,
            { kind: 'say', text: '2 tightened · 1 deferred. Spec updated.', tone: 'ok' },
        ],
    },
    {
        id: 'plan',
        label: 'plan',
        command: '/atomic-plan COPY INTO + FILE FORMAT lineage',
        output: [
            { kind: 'say', text: 'Non-trivial. Writing a design doc,' },
            { kind: 'say', text: 'then a spec the build loop reads.' },
            gap,
            { kind: 'tool', text: 'Write(docs/design/snowflake-copy.md)' },
            { kind: 'tool', text: 'Write(docs/spec/snowflake-copy.md)' },
            gap,
            { kind: 'say', text: 'Spec breaks into 3 checkpoints:' },
            { kind: 'out', text: '1  COPY INTO @stage → table edge' },
            { kind: 'out', text: '2  FILE FORMAT → file_format node' },
            { kind: 'out', text: '3  stage → column lineage' },
            gap,
            { kind: 'say', text: 'Spec written. Approve to build.', tone: 'ok' },
        ],
    },
    {
        id: 'implement',
        label: 'implement',
        command: '/subagent-implementation',
        output: [
            { kind: 'say', text: 'Each checkpoint: implementer writes' },
            { kind: 'say', text: 'test-first, reviewer gates it.' },
            gap,
            { kind: 'tool', text: 'Task(atomic-implementer, cp 1/3)' },
            { kind: 'out', text: 'failing test → COPY INTO edge → pass' },
            { kind: 'cont', text: 'sql.go  +31 −2' },
            { kind: 'tool', text: 'Task(atomic-reviewer, cp 1/3)' },
            { kind: 'out', text: 'missing the @~ / @% stage forms.' },
            { kind: 'cont', text: 'CHANGES_REQUESTED', tone: 'warn' },
            { kind: 'tool', text: 'Task(atomic-implementer, cp 1/3)' },
            { kind: 'out', text: 'added @~ @% forms · test green' },
            { kind: 'tool', text: 'Task(atomic-reviewer, cp 1/3)' },
            { kind: 'out', text: 'APPROVED', tone: 'ok' },
            gap,
            { kind: 'say', text: 'cp 2/3, cp 3/3 — same loop:' },
            { kind: 'out', text: 'implementer → reviewer → APPROVED', tone: 'ok' },
            gap,
            { kind: 'tool', text: 'Bash(go test ./...)' },
            { kind: 'out', text: 'ok   414 passed', tone: 'ok' },
        ],
    },
    {
        id: 'ship',
        label: 'ship',
        command: '/commit pr',
        output: [
            { kind: 'say', text: 'Staging, writing the message,' },
            { kind: 'say', text: 'opening a PR.' },
            gap,
            { kind: 'tool', text: 'Bash(git commit)' },
            { kind: 'out', text: 'feat(code-intel): COPY INTO + FILE' },
            { kind: 'cont', text: 'FORMAT lineage for Snowflake' },
            { kind: 'cont', text: '3 files  +61 −4   a3f9c21' },
            { kind: 'tool', text: 'Bash(git push -u origin)' },
            { kind: 'out', text: '→ snowflake-copy-lineage' },
            { kind: 'tool', text: 'Bash(gh pr create)' },
            { kind: 'out', text: 'github.com/…/pull/71', tone: 'ok' },
            gap,
            { kind: 'say', text: 'PR #71 opened.', tone: 'ok' },
        ],
    },
    {
        id: 'autopilot',
        label: 'autopilot',
        command: '/autopilot 142 squash-and-merge',
        output: [
            { kind: 'say', text: 'One command runs the whole' },
            { kind: 'say', text: 'lifecycle on issue #142, hands-off.' },
            gap,
            { kind: 'out', text: '#142  embedded SQL in Kotlin', tone: 'muted' },
            { kind: 'say', text: 'plan → spec, 2 checkpoints' },
            { kind: 'say', text: 'implement → builder ⇄ reviewer, green' },
            { kind: 'say', text: 'review → fresh context, gated on spec' },
            { kind: 'say', text: 'ship → squash, merge, close' },
            gap,
            { kind: 'say', text: '#142 merged. You chose how. Done.', tone: 'ok' },
        ],
    },
]

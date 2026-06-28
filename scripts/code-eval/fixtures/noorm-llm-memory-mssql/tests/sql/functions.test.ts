/**
 * Scalar function tests via ctx.func(...).
 *
 * Each function gets a happy + boundary case. Bypasses the facade and
 * Zod so the SQL contract is exercised directly. fn_MemoryRank uses a
 * Kysely raw selectNoFrom() because ctx.func can't easily express a
 * scalar number return without a wrapping object — but we still get
 * the SQL contract under test.
 */
import { beforeAll, beforeEach, describe, expect, it } from 'bun:test';

import { bootstrap, resetApplicationData } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});

beforeEach(async () => {

    await resetApplicationData(ctx);

});

async function makeMilestone(): Promise<number> {

    const rows = await ctx.proc('sp_Milestone_Create', {
        title: 'fn-test',
        content: '',
        reason: '',
        provenance_id: 0,
        agent_id: 0,
    });

    const created = rows[0];

    if (!created) {

        throw new Error('sp_Milestone_Create returned no rows');
    }

    return created.milestone_id;

}

async function makeTask(milestoneId: number, title = 't'): Promise<{ milestone_id: number; task_no: number }> {

    const rows = await ctx.proc('sp_Task_Create', {
        milestone_id: milestoneId,
        title,
        content: '',
        reason: '',
        agent_id: 0,
    });

    const created = rows[0];

    if (!created) {

        throw new Error('sp_Task_Create returned no rows');
    }

    return created;

}

async function makeMemory(flags: Partial<{
    was_inferred: boolean;
    was_observed: boolean;
    was_evidenced: boolean;
    was_user_provided: boolean;
}>): Promise<number> {

    const rows = await ctx.proc('sp_Memory_Create', {
        content: 'fn-test-memory',
        domain: 'backend',
        category: 'fact',
        reason: '',
        provenance_id: 0,
        agent_id: 0,
        was_inferred:      flags.was_inferred      ?? false,
        was_observed:      flags.was_observed      ?? false,
        was_evidenced:     flags.was_evidenced     ?? false,
        was_user_provided: flags.was_user_provided ?? false,
    });

    const created = rows[0];

    if (!created) {

        throw new Error('sp_Memory_Create returned no rows');
    }

    return created.memory_id;

}

describe('sql: fn_NextTaskNo', () => {

    it('returns 1 when no tasks exist for the milestone', async () => {

        const id = await makeMilestone();

        const r = await ctx.func('fn_NextTaskNo', { milestone_id: id }, 'next');

        expect(r.next).toBe(1);

    });

    it('returns max(task_no)+1 when tasks already exist', async () => {

        const id = await makeMilestone();

        await makeTask(id, 'a');
        await makeTask(id, 'b');
        await makeTask(id, 'c');

        const r = await ctx.func('fn_NextTaskNo', { milestone_id: id }, 'next');

        expect(r.next).toBe(4);

    });

});

describe('sql: fn_IsActive', () => {

    it('returns true for relevance_status = active and false otherwise', async () => {

        // BIT is surfaced as boolean by tedious.
        const active = await ctx.kysely
            .selectNoFrom((eb) => eb.fn<boolean>('dbo.fn_IsActive', [eb.val('active')]).as('v'))
            .executeTakeFirstOrThrow();

        const deleted = await ctx.kysely
            .selectNoFrom((eb) => eb.fn<boolean>('dbo.fn_IsActive', [eb.val('deleted')]).as('v'))
            .executeTakeFirstOrThrow();

        const needsReview = await ctx.kysely
            .selectNoFrom((eb) => eb.fn<boolean>('dbo.fn_IsActive', [eb.val('needs-review')]).as('v'))
            .executeTakeFirstOrThrow();

        expect(active.v).toBe(true);
        expect(deleted.v).toBe(false);
        expect(needsReview.v).toBe(false);

    });

});

describe('sql: fn_IsOpen', () => {

    it('returns true for in-progress and false for done/abandoned', async () => {

        const inProgress = await ctx.kysely
            .selectNoFrom((eb) => eb.fn<boolean>('dbo.fn_IsOpen', [eb.val('in-progress')]).as('v'))
            .executeTakeFirstOrThrow();

        const done = await ctx.kysely
            .selectNoFrom((eb) => eb.fn<boolean>('dbo.fn_IsOpen', [eb.val('done')]).as('v'))
            .executeTakeFirstOrThrow();

        const abandoned = await ctx.kysely
            .selectNoFrom((eb) => eb.fn<boolean>('dbo.fn_IsOpen', [eb.val('abandoned')]).as('v'))
            .executeTakeFirstOrThrow();

        const notStarted = await ctx.kysely
            .selectNoFrom((eb) => eb.fn<boolean>('dbo.fn_IsOpen', [eb.val('not-started')]).as('v'))
            .executeTakeFirstOrThrow();

        expect(inProgress.v).toBe(true);
        expect(done.v).toBe(false);
        expect(abandoned.v).toBe(false);
        expect(notStarted.v).toBe(true);

    });

});

describe('sql: fn_MemoryConfidence', () => {

    it('returns the count of true was_* booleans (0..4)', async () => {

        const zero = await makeMemory({});
        const two  = await makeMemory({ was_observed: true, was_evidenced: true });
        const four = await makeMemory({
            was_inferred: true, was_observed: true,
            was_evidenced: true, was_user_provided: true,
        });

        const r0 = await ctx.func('fn_MemoryConfidence', { memory_id: zero }, 'confidence');
        const r2 = await ctx.func('fn_MemoryConfidence', { memory_id: two }, 'confidence');
        const r4 = await ctx.func('fn_MemoryConfidence', { memory_id: four }, 'confidence');

        expect(r0.confidence).toBe(0);
        expect(r2.confidence).toBe(2);
        expect(r4.confidence).toBe(4);

    });

});

describe('sql: fn_TaskDependencyWouldCycle', () => {

    it('returns false for a fresh edge that does not close a cycle', async () => {

        const milestoneId = await makeMilestone();
        const a = await makeTask(milestoneId, 'a');
        const b = await makeTask(milestoneId, 'b');

        const r = await ctx.func('fn_TaskDependencyWouldCycle', {
            milestone_id:     a.milestone_id,
            task_no:          a.task_no,
            dep_milestone_id: b.milestone_id,
            dep_task_no:      b.task_no,
        }, 'cycles');

        // BIT comes back as boolean | 0 | 1 depending on driver.
        expect(r.cycles === false || r.cycles === 0).toBe(true);

    });

    it('returns true when the proposed edge would close a cycle', async () => {

        const milestoneId = await makeMilestone();
        const a = await makeTask(milestoneId, 'a');
        const b = await makeTask(milestoneId, 'b');

        // a -> b already exists. Proposing b -> a closes the loop.
        await ctx.proc('sp_Task_Depend', {
            milestone_id:     a.milestone_id, task_no:     a.task_no,
            dep_milestone_id: b.milestone_id, dep_task_no: b.task_no,
            dependency_verb:  'blocks',
            reason:           'a needs b',
        });

        const r = await ctx.func('fn_TaskDependencyWouldCycle', {
            milestone_id:     b.milestone_id,
            task_no:          b.task_no,
            dep_milestone_id: a.milestone_id,
            dep_task_no:      a.task_no,
        }, 'cycles');

        expect(r.cycles === true || r.cycles === 1).toBe(true);

    });

});

describe('sql: fn_MemoryRank', () => {

    it('returns a positive number for an active memory with confidence', async () => {

        const id = await makeMemory({
            was_observed: true, was_evidenced: true, was_user_provided: true,
        });

        const r = await ctx.kysely
            .selectNoFrom((eb) => eb.fn<number>('dbo.fn_MemoryRank', [eb.val(id)]).as('rank'))
            .executeTakeFirstOrThrow();

        expect(typeof r.rank).toBe('number');
        expect(r.rank).toBeGreaterThan(0);
        expect(r.rank).toBeLessThanOrEqual(1);

    });

    it('returns 0 when the relevance weight is 0 (irrelevant memory)', async () => {

        const id = await makeMemory({ was_observed: true });

        await ctx.proc('sp_Memory_SetRelevance', {
            memory_id: id, new_relevance_status: 'irrelevant',
            agent_id: 0, reason: 'no longer needed',
        });

        const r = await ctx.kysely
            .selectNoFrom((eb) => eb.fn<number>('dbo.fn_MemoryRank', [eb.val(id)]).as('rank'))
            .executeTakeFirstOrThrow();

        expect(r.rank).toBe(0);

    });

});

describe('sql: fn_NoteSubtypeCount', () => {

    it('returns 1 for a normally-created note (one subtype row)', async () => {

        const milestoneRows = await ctx.proc('sp_Milestone_Create', {
            title: 'note-host', content: '', reason: '',
            provenance_id: 0, agent_id: 0,
        });

        const m = milestoneRows[0];

        if (!m) {

            throw new Error('milestone create failed');
        }

        const noteRows = await ctx.proc('sp_Note_Create_Milestone', {
            content: 'note', reason: '', provenance_id: 0, agent_id: 0,
            milestone_id: m.milestone_id,
        });

        const note = noteRows[0];

        if (!note) {

            throw new Error('note create failed');
        }

        const r = await ctx.func('fn_NoteSubtypeCount', { note_id: note.note_id }, 'count');

        expect(r.count).toBe(1);

    });

    it('returns 0 for a note_id that has no subtype row', async () => {

        // No note exists at id 99999, so all three subtype tables miss.
        const r = await ctx.func('fn_NoteSubtypeCount', { note_id: 99999 }, 'count');

        expect(r.count).toBe(0);

    });

});

describe('sql: fn_NoteMatchesSubtype', () => {

    it('returns 1 when the declared note_type matches the actual subtype row', async () => {

        const milestoneRows = await ctx.proc('sp_Milestone_Create', {
            title: 'note-host', content: '', reason: '',
            provenance_id: 0, agent_id: 0,
        });

        const m = milestoneRows[0];

        if (!m) {

            throw new Error('milestone create failed');
        }

        const noteRows = await ctx.proc('sp_Note_Create_Milestone', {
            content: 'note', reason: '', provenance_id: 0, agent_id: 0,
            milestone_id: m.milestone_id,
        });

        const note = noteRows[0];

        if (!note) {

            throw new Error('note create failed');
        }

        const matched = await ctx.func('fn_NoteMatchesSubtype', {
            note_id: note.note_id, note_type: 'milestone',
        }, 'matches');

        const mismatched = await ctx.func('fn_NoteMatchesSubtype', {
            note_id: note.note_id, note_type: 'project',
        }, 'matches');

        // BIT is surfaced as boolean by tedious — the type contract says so.
        expect(matched.matches).toBe(true);
        expect(mismatched.matches).toBe(false);

    });

});

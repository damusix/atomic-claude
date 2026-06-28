import { beforeAll, beforeEach, describe, it, expect } from 'bun:test';
import { sql } from 'kysely';

import { bootstrap, truncateAll } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});

beforeEach(async () => {

    await truncateAll(ctx);

});

async function makeMilestone(): Promise<number> {

    const result = await ctx.proc('sp_Milestone_Create', {
        p_title: 'M', p_content: '', p_reason: '',
        p_provenance_id: 0, p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('milestone create failed');
    return created.milestone_id;

}

async function makeTask(milestoneId: number, title = 'T'): Promise<{ milestone_id: number; task_no: number }> {

    const result = await ctx.proc('sp_Task_Create', {
        p_milestone_id: milestoneId, p_title: title, p_content: '',
        p_reason: '', p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('task create failed');
    return created;

}

async function makeMemory(flags: Partial<{
    was_inferred: boolean;
    was_observed: boolean;
    was_evidenced: boolean;
    was_user_provided: boolean;
}>): Promise<number> {

    const result = await ctx.proc('sp_Memory_Create', {
        p_content: 'm', p_domain: 'backend', p_category: 'fact',
        p_reason: '', p_provenance_id: 0, p_agent_id: 0,
        p_was_inferred:      flags.was_inferred      ?? false,
        p_was_observed:      flags.was_observed      ?? false,
        p_was_evidenced:     flags.was_evidenced     ?? false,
        p_was_user_provided: flags.was_user_provided ?? false,
    });
    const [created] = result;
    if (!created) throw new Error('memory create failed');
    return created.memory_id;

}

/**
 * Invoke a scalar SQL function not declared in the SDK's `Funcs` catalog
 * via the raw Kysely `sql` tag. Returns the single scalar result. Used for
 * functions like fn_IsActive / fn_IsOpen / fn_NoteSubtypeCount /
 * fn_NoteMatchesSubtype that take typed scalar args and return a single value.
 *
 * @example
 *   const v = await callBoolFn('fn_IsActive', sql`${'active'}::varchar`);
 */
async function callBoolFn(fn: string, arg: ReturnType<typeof sql>): Promise<boolean> {

    const result = await sql<{ result: boolean }>`SELECT ${sql.raw(`"${fn}"`)}(${arg}) AS result`
        .execute(ctx.kysely);
    const [row] = result.rows;
    if (!row) throw new Error(`${fn} returned no rows`);

    return row.result;

}

async function callIntFn(fn: string, arg: ReturnType<typeof sql>): Promise<number> {

    const result = await sql<{ result: number }>`SELECT ${sql.raw(`"${fn}"`)}(${arg}) AS result`
        .execute(ctx.kysely);
    const [row] = result.rows;
    if (!row) throw new Error(`${fn} returned no rows`);

    return row.result;

}

async function callBoolFn2(fn: string, a: ReturnType<typeof sql>, b: ReturnType<typeof sql>): Promise<boolean> {

    const result = await sql<{ result: boolean }>`SELECT ${sql.raw(`"${fn}"`)}(${a}, ${b}) AS result`
        .execute(ctx.kysely);
    const [row] = result.rows;
    if (!row) throw new Error(`${fn} returned no rows`);

    return row.result;

}

describe('fn_NextTaskNo', () => {

    it('returns 1 for an empty milestone', async () => {

        const id = await makeMilestone();

        const r = await ctx.func('fn_NextTaskNo', { p_milestone_id: id }, 'fn_NextTaskNo');
        expect(r.fn_NextTaskNo).toBe(1);

    });

    it('returns MAX(task_no)+1 once tasks exist', async () => {

        const id = await makeMilestone();

        await makeTask(id);
        await makeTask(id);
        await makeTask(id);

        const r = await ctx.func('fn_NextTaskNo', { p_milestone_id: id }, 'fn_NextTaskNo');
        expect(r.fn_NextTaskNo).toBe(4);

    });

});

describe('fn_MemoryConfidence', () => {

    it('returns 0 when all flags are false', async () => {

        const id = await makeMemory({});

        const r = await ctx.func('fn_MemoryConfidence', { p_memory_id: id }, 'confidence');
        expect(r.confidence).toBe(0);

    });

    it('returns 1 when exactly one flag is true', async () => {

        const id = await makeMemory({ was_observed: true });

        const r = await ctx.func('fn_MemoryConfidence', { p_memory_id: id }, 'confidence');
        expect(r.confidence).toBe(1);

    });

    it('returns 2 when two flags are true', async () => {

        const id = await makeMemory({ was_observed: true, was_evidenced: true });

        const r = await ctx.func('fn_MemoryConfidence', { p_memory_id: id }, 'confidence');
        expect(r.confidence).toBe(2);

    });

    it('returns 3 when three flags are true', async () => {

        const id = await makeMemory({
            was_observed: true, was_evidenced: true, was_user_provided: true,
        });

        const r = await ctx.func('fn_MemoryConfidence', { p_memory_id: id }, 'confidence');
        expect(r.confidence).toBe(3);

    });

    it('returns 4 when all flags are true', async () => {

        const id = await makeMemory({
            was_inferred: true, was_observed: true,
            was_evidenced: true, was_user_provided: true,
        });

        const r = await ctx.func('fn_MemoryConfidence', { p_memory_id: id }, 'confidence');
        expect(r.confidence).toBe(4);

    });

});

describe('fn_IsTrackingTransitionAllowed', () => {

    it('returns true for a known allowed pair (in-progress -> done)', async () => {

        const r = await ctx.func('fn_IsTrackingTransitionAllowed', {
            p_from_status: 'in-progress', p_to_status: 'done',
        }, 'allowed');

        expect(r.allowed).toBe(true);

    });

    it('returns false for a disallowed pair (not-started -> done)', async () => {

        const r = await ctx.func('fn_IsTrackingTransitionAllowed', {
            p_from_status: 'not-started', p_to_status: 'done',
        }, 'allowed');

        expect(r.allowed).toBe(false);

    });

});

describe('fn_IsRelevanceTransitionAllowed', () => {

    it('returns true for a known allowed pair (active -> needs-review)', async () => {

        const r = await ctx.func('fn_IsRelevanceTransitionAllowed', {
            p_from_status: 'active', p_to_status: 'needs-review',
        }, 'allowed');

        expect(r.allowed).toBe(true);

    });

    it('returns false for a disallowed pair (active -> active)', async () => {

        const r = await ctx.func('fn_IsRelevanceTransitionAllowed', {
            p_from_status: 'active', p_to_status: 'active',
        }, 'allowed');

        expect(r.allowed).toBe(false);

    });

});

describe('fn_TaskDependencyWouldCycle', () => {

    it('detects a cycle when A depends on B and we attempt B -> A', async () => {

        const milestoneId = await makeMilestone();
        const a = await makeTask(milestoneId, 'a');
        const b = await makeTask(milestoneId, 'b');

        await ctx.proc('sp_Task_Depend', {
            p_milestone_id:     a.milestone_id, p_task_no:     a.task_no,
            p_dep_milestone_id: b.milestone_id, p_dep_task_no: b.task_no,
            p_dependency_verb:  'blocks',
            p_reason:           'a needs b',
        });

        const r = await ctx.func('fn_TaskDependencyWouldCycle', {
            p_milestone_id:     b.milestone_id,
            p_task_no:          b.task_no,
            p_dep_milestone_id: a.milestone_id,
            p_dep_task_no:      a.task_no,
        }, 'fn_TaskDependencyWouldCycle');

        expect(r.fn_TaskDependencyWouldCycle).toBe(true);

    });

    it('returns false when no cycle would be created', async () => {

        const milestoneId = await makeMilestone();
        const a = await makeTask(milestoneId, 'a');
        const b = await makeTask(milestoneId, 'b');

        const r = await ctx.func('fn_TaskDependencyWouldCycle', {
            p_milestone_id:     a.milestone_id,
            p_task_no:          a.task_no,
            p_dep_milestone_id: b.milestone_id,
            p_dep_task_no:      b.task_no,
        }, 'fn_TaskDependencyWouldCycle');

        expect(r.fn_TaskDependencyWouldCycle).toBe(false);

    });

});

describe('fn_MemoryRank', () => {

    it('ranks high-confidence memory above low-confidence memory when both are freshly accessed', async () => {

        // High-confidence: 3 flags (still inferred=false to keep "grounded" higher).
        const high = await makeMemory({
            was_observed: true, was_evidenced: true, was_user_provided: true,
        });
        // Low-confidence: 1 flag.
        const low = await makeMemory({ was_inferred: true });

        const highRank = await ctx.func('fn_MemoryRank', { p_memory_id: high }, 'rank');
        const lowRank = await ctx.func('fn_MemoryRank', { p_memory_id: low }, 'rank');

        expect(highRank.rank).toBeGreaterThan(0);
        expect(lowRank.rank).toBeGreaterThan(0);
        expect(highRank.rank).toBeGreaterThan(lowRank.rank);

    });

    it('ranks a freshly accessed memory above one with stale last_accessed_at (recency decay)', async () => {

        // Both have identical confidence so the only differentiator is recency.
        const stale = await makeMemory({ was_observed: true, was_evidenced: true });
        const fresh = await makeMemory({ was_observed: true, was_evidenced: true });

        // Backdate the stale memory's last_accessed_at to 60 days ago via direct UPDATE.
        await ctx.kysely.updateTable('Memory')
            .set({ last_accessed_at: sql<Date>`NOW() - INTERVAL '60 days'` })
            .where('memory_id', '=', stale)
            .execute();

        const staleRank = await ctx.func('fn_MemoryRank', { p_memory_id: stale }, 'rank');
        const freshRank = await ctx.func('fn_MemoryRank', { p_memory_id: fresh }, 'rank');

        expect(staleRank.rank).toBeGreaterThan(0);
        expect(freshRank.rank).toBeGreaterThan(staleRank.rank);

    });

    it('returns 0 for an irrelevant memory (relevance weight = 0)', async () => {

        const id = await makeMemory({ was_observed: true });

        // active -> irrelevant is allowed.
        await ctx.proc('sp_Memory_SetRelevance', {
            p_memory_id: id, p_new_relevance_status: 'irrelevant',
            p_agent_id: 0, p_reason: 'no longer needed',
        });

        const r = await ctx.func('fn_MemoryRank', { p_memory_id: id }, 'rank');

        expect(r.rank).toBe(0);

    });

});

describe('fn_NoteSubtypeCount', () => {

    it('returns 1 for a properly-inserted project note', async () => {

        const result = await ctx.proc('sp_Note_Create_Project', {
            p_content: 'pn', p_reason: '',
            p_provenance_id: 0, p_agent_id: 0, p_project_id: 0,
        });
        const [row] = result;
        if (!row) throw new Error('note create failed');

        const count = await callIntFn('fn_NoteSubtypeCount', sql`${row.note_id}::int`);
        expect(count).toBe(1);

    });

    it('returns 1 for a properly-inserted milestone note', async () => {

        const milestoneId = await makeMilestone();

        const result = await ctx.proc('sp_Note_Create_Milestone', {
            p_content: 'mn', p_reason: '',
            p_provenance_id: 0, p_agent_id: 0, p_milestone_id: milestoneId,
        });
        const [row] = result;
        if (!row) throw new Error('note create failed');

        const count = await callIntFn('fn_NoteSubtypeCount', sql`${row.note_id}::int`);
        expect(count).toBe(1);

    });

    it('returns 1 for a properly-inserted task note', async () => {

        const milestoneId = await makeMilestone();
        const task = await makeTask(milestoneId);

        const result = await ctx.proc('sp_Note_Create_Task', {
            p_content: 'tn', p_reason: '',
            p_provenance_id: 0, p_agent_id: 0,
            p_milestone_id: task.milestone_id, p_task_no: task.task_no,
        });
        const [row] = result;
        if (!row) throw new Error('note create failed');

        const count = await callIntFn('fn_NoteSubtypeCount', sql`${row.note_id}::int`);
        expect(count).toBe(1);

    });

    it('returns 0 for an unknown note_id', async () => {

        const count = await callIntFn('fn_NoteSubtypeCount', sql`${999999}::int`);
        expect(count).toBe(0);

    });

});

describe('fn_NoteMatchesSubtype', () => {

    it('returns true for a project note declared as project', async () => {

        const result = await ctx.proc('sp_Note_Create_Project', {
            p_content: 'pn', p_reason: '',
            p_provenance_id: 0, p_agent_id: 0, p_project_id: 0,
        });
        const [row] = result;
        if (!row) throw new Error('note create failed');

        const ok = await callBoolFn2(
            'fn_NoteMatchesSubtype',
            sql`${row.note_id}::int`,
            sql`${'project'}::varchar`,
        );
        expect(ok).toBe(true);

    });

    it('returns true for a milestone note declared as milestone', async () => {

        const milestoneId = await makeMilestone();
        const result = await ctx.proc('sp_Note_Create_Milestone', {
            p_content: 'mn', p_reason: '',
            p_provenance_id: 0, p_agent_id: 0, p_milestone_id: milestoneId,
        });
        const [row] = result;
        if (!row) throw new Error('note create failed');

        const ok = await callBoolFn2(
            'fn_NoteMatchesSubtype',
            sql`${row.note_id}::int`,
            sql`${'milestone'}::varchar`,
        );
        expect(ok).toBe(true);

    });

    it('returns true for a task note declared as task', async () => {

        const milestoneId = await makeMilestone();
        const task = await makeTask(milestoneId);
        const result = await ctx.proc('sp_Note_Create_Task', {
            p_content: 'tn', p_reason: '',
            p_provenance_id: 0, p_agent_id: 0,
            p_milestone_id: task.milestone_id, p_task_no: task.task_no,
        });
        const [row] = result;
        if (!row) throw new Error('note create failed');

        const ok = await callBoolFn2(
            'fn_NoteMatchesSubtype',
            sql`${row.note_id}::int`,
            sql`${'task'}::varchar`,
        );
        expect(ok).toBe(true);

    });

    it('returns false when the declared note_type does not match the subtype table', async () => {

        const result = await ctx.proc('sp_Note_Create_Project', {
            p_content: 'pn', p_reason: '',
            p_provenance_id: 0, p_agent_id: 0, p_project_id: 0,
        });
        const [row] = result;
        if (!row) throw new Error('note create failed');

        // The note exists in Project_Note, but we declare it as 'milestone' →
        // the function looks in Milestone_Note and finds nothing.
        const ok = await callBoolFn2(
            'fn_NoteMatchesSubtype',
            sql`${row.note_id}::int`,
            sql`${'milestone'}::varchar`,
        );
        expect(ok).toBe(false);

    });

    it('returns false for an unknown note_type discriminator', async () => {

        const ok = await callBoolFn2(
            'fn_NoteMatchesSubtype',
            sql`${1}::int`,
            sql`${'not-a-real-type'}::varchar`,
        );
        expect(ok).toBe(false);

    });

});

describe('fn_IsActive', () => {

    it('returns true only for relevance_status = active', async () => {

        const trueCases = ['active'];
        const falseCases = ['needs-review', 'superseded', 'irrelevant', 'deleted', ''];

        for (const status of trueCases) {

            const got = await callBoolFn('fn_IsActive', sql`${status}::varchar`);
            expect(got).toBe(true);

        }

        for (const status of falseCases) {

            const got = await callBoolFn('fn_IsActive', sql`${status}::varchar`);
            expect(got).toBe(false);

        }

    });

});

describe('fn_IsOpen', () => {

    it('returns true for any tracking_status except done and abandoned', async () => {

        const trueCases = [
            'not-started', 'in-progress', 'agent-review',
            'human-review', 'needs-more-work',
        ];
        const falseCases = ['done', 'abandoned'];

        for (const status of trueCases) {

            const got = await callBoolFn('fn_IsOpen', sql`${status}::varchar`);
            expect(got).toBe(true);

        }

        for (const status of falseCases) {

            const got = await callBoolFn('fn_IsOpen', sql`${status}::varchar`);
            expect(got).toBe(false);

        }

    });

});

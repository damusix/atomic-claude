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

/**
 * Reference-table CRUD procs are not yet declared in the project's
 * generated `Procs` catalog, so we invoke them via the raw Kysely `sql`
 * tag rather than `ctx.proc('sp_Ref_…')`.
 *
 * Once the catalog regenerates these names can be migrated to typed calls.
 */
async function callRefCreateMemoryDomain(domain: string): Promise<void> {

    await sql`CALL "sp_Ref_Create_MemoryDomain"(${domain})`.execute(ctx.kysely);

}

async function callRefDeleteMemoryDomain(domain: string): Promise<void> {

    await sql`CALL "sp_Ref_Delete_MemoryDomain"(${domain})`.execute(ctx.kysely);

}

async function callRefCreateMemoryCategory(category: string): Promise<void> {

    await sql`CALL "sp_Ref_Create_MemoryCategory"(${category})`.execute(ctx.kysely);

}

async function callRefDeleteMemoryCategory(category: string): Promise<void> {

    await sql`CALL "sp_Ref_Delete_MemoryCategory"(${category})`.execute(ctx.kysely);

}

async function callRefCreateDependencyVerb(verb: string): Promise<void> {

    await sql`CALL "sp_Ref_Create_DependencyVerb"(${verb})`.execute(ctx.kysely);

}

async function callRefDeleteDependencyVerb(verb: string): Promise<void> {

    await sql`CALL "sp_Ref_Delete_DependencyVerb"(${verb})`.execute(ctx.kysely);

}

async function createMemoryUsing(domain: string, category: string): Promise<number> {

    const result = await ctx.proc('sp_Memory_Create', {
        p_content:           'ref-test memory',
        p_domain:            domain,
        p_category:          category,
        p_reason:            'ref fixture',
        p_provenance_id:     0,
        p_agent_id:          0,
        p_was_inferred:      false,
        p_was_observed:      true,
        p_was_evidenced:     false,
        p_was_user_provided: false,
    });
    const [created] = result;
    if (!created) throw new Error('sp_Memory_Create returned no rows');
    return created.memory_id;

}

describe('sp_Ref_Create_MemoryDomain', () => {

    it('inserts a new domain row', async () => {

        await callRefCreateMemoryDomain('mobile');

        const row = await ctx.kysely.selectFrom('MemoryDomain')
            .selectAll()
            .where('domain', '=', 'mobile')
            .executeTakeFirstOrThrow();

        expect(row.domain).toBe('mobile');

    });

    it('is idempotent on duplicate insert (ON CONFLICT DO NOTHING)', async () => {

        // 'backend' is already seeded — proc swallows the conflict rather
        // than raising 23505. The brief mentions a duplicate-rejection test;
        // asserting the proc's documented behaviour instead.
        await callRefCreateMemoryDomain('backend');

        const rows = await ctx.kysely.selectFrom('MemoryDomain')
            .selectAll()
            .where('domain', '=', 'backend')
            .execute();

        expect(rows.length).toBe(1);

    });

});

describe('sp_Ref_Delete_MemoryDomain', () => {

    it('removes an unused domain', async () => {

        await callRefCreateMemoryDomain('mobile');
        await callRefDeleteMemoryDomain('mobile');

        const row = await ctx.kysely.selectFrom('MemoryDomain')
            .selectAll()
            .where('domain', '=', 'mobile')
            .executeTakeFirst();

        expect(row).toBeUndefined();

    });

    it('rejects deletion when at least one Memory still references the domain', async () => {

        await createMemoryUsing('backend', 'fact');

        await expect(
            callRefDeleteMemoryDomain('backend'),
        ).rejects.toThrow(/MemoryDomain backend is in use and cannot be deleted/);

        const row = await ctx.kysely.selectFrom('MemoryDomain')
            .selectAll()
            .where('domain', '=', 'backend')
            .executeTakeFirstOrThrow();

        expect(row.domain).toBe('backend');

    });

});

describe('sp_Ref_Create_MemoryCategory', () => {

    it('inserts a new category row', async () => {

        await callRefCreateMemoryCategory('risk');

        const row = await ctx.kysely.selectFrom('MemoryCategory')
            .selectAll()
            .where('category', '=', 'risk')
            .executeTakeFirstOrThrow();

        expect(row.category).toBe('risk');

    });

    it('is idempotent on duplicate insert (ON CONFLICT DO NOTHING)', async () => {

        await callRefCreateMemoryCategory('fact');

        const rows = await ctx.kysely.selectFrom('MemoryCategory')
            .selectAll()
            .where('category', '=', 'fact')
            .execute();

        expect(rows.length).toBe(1);

    });

});

describe('sp_Ref_Delete_MemoryCategory', () => {

    it('removes an unused category', async () => {

        await callRefCreateMemoryCategory('risk');
        await callRefDeleteMemoryCategory('risk');

        const row = await ctx.kysely.selectFrom('MemoryCategory')
            .selectAll()
            .where('category', '=', 'risk')
            .executeTakeFirst();

        expect(row).toBeUndefined();

    });

    it('rejects deletion when at least one Memory still references the category', async () => {

        await createMemoryUsing('backend', 'fact');

        await expect(
            callRefDeleteMemoryCategory('fact'),
        ).rejects.toThrow(/MemoryCategory fact is in use and cannot be deleted/);

        const row = await ctx.kysely.selectFrom('MemoryCategory')
            .selectAll()
            .where('category', '=', 'fact')
            .executeTakeFirstOrThrow();

        expect(row.category).toBe('fact');

    });

});

describe('sp_Ref_Create_DependencyVerb', () => {

    it('inserts a new dependency verb row', async () => {

        await callRefCreateDependencyVerb('supports');

        const row = await ctx.kysely.selectFrom('DependencyVerb')
            .selectAll()
            .where('dependency_verb', '=', 'supports')
            .executeTakeFirstOrThrow();

        expect(row.dependency_verb).toBe('supports');

    });

    it('is idempotent on duplicate insert (ON CONFLICT DO NOTHING)', async () => {

        await callRefCreateDependencyVerb('blocks');

        const rows = await ctx.kysely.selectFrom('DependencyVerb')
            .selectAll()
            .where('dependency_verb', '=', 'blocks')
            .execute();

        expect(rows.length).toBe(1);

    });

});

describe('sp_Ref_Delete_DependencyVerb', () => {

    it('removes an unused dependency verb', async () => {

        await callRefCreateDependencyVerb('supports');
        await callRefDeleteDependencyVerb('supports');

        const row = await ctx.kysely.selectFrom('DependencyVerb')
            .selectAll()
            .where('dependency_verb', '=', 'supports')
            .executeTakeFirst();

        expect(row).toBeUndefined();

    });

    it('rejects deletion when at least one Task_Dependency still references the verb', async () => {

        const milestoneA = await ctx.proc('sp_Milestone_Create', {
            p_title: 'M-A', p_content: '', p_reason: 'fixture',
            p_provenance_id: 0, p_agent_id: 0,
        });
        const [milestoneARow] = milestoneA;
        if (!milestoneARow) throw new Error('milestone create failed');

        const taskA = await ctx.proc('sp_Task_Create', {
            p_milestone_id: milestoneARow.milestone_id, p_title: 'T-A', p_content: '',
            p_reason: 'fixture', p_agent_id: 0,
        });
        const [taskARow] = taskA;
        if (!taskARow) throw new Error('task create failed');

        const taskB = await ctx.proc('sp_Task_Create', {
            p_milestone_id: milestoneARow.milestone_id, p_title: 'T-B', p_content: '',
            p_reason: 'fixture', p_agent_id: 0,
        });
        const [taskBRow] = taskB;
        if (!taskBRow) throw new Error('task create failed');

        await ctx.proc('sp_Task_Depend', {
            p_milestone_id:     taskARow.milestone_id,
            p_task_no:          taskARow.task_no,
            p_dep_milestone_id: taskBRow.milestone_id,
            p_dep_task_no:      taskBRow.task_no,
            p_dependency_verb:  'blocks',
            p_reason:           'fixture dependency',
        });

        await expect(
            callRefDeleteDependencyVerb('blocks'),
        ).rejects.toThrow(/DependencyVerb blocks is in use and cannot be deleted/);

        const row = await ctx.kysely.selectFrom('DependencyVerb')
            .selectAll()
            .where('dependency_verb', '=', 'blocks')
            .executeTakeFirstOrThrow();

        expect(row.dependency_verb).toBe('blocks');

    });

});

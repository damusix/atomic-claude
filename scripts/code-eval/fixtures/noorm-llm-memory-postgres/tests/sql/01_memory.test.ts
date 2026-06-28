import { beforeAll, beforeEach, describe, it, expect } from 'bun:test';

import { bootstrap, truncateAll } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});

beforeEach(async () => {

    await truncateAll(ctx);

});

async function createMemory(overrides: Partial<{
    content: string;
    domain: string;
    category: string;
    was_inferred: boolean;
    was_observed: boolean;
    was_evidenced: boolean;
    was_user_provided: boolean;
}> = {}): Promise<number> {

    const result = await ctx.proc('sp_Memory_Create', {
        p_content:           overrides.content           ?? 'baseline memory',
        p_domain:            overrides.domain            ?? 'backend',
        p_category:          overrides.category          ?? 'fact',
        p_reason:            'sql test fixture',
        p_provenance_id:     0,
        p_agent_id:          0,
        p_was_inferred:      overrides.was_inferred      ?? false,
        p_was_observed:      overrides.was_observed      ?? true,
        p_was_evidenced:     overrides.was_evidenced     ?? false,
        p_was_user_provided: overrides.was_user_provided ?? false,
    });

    const [created] = result;
    if (!created) throw new Error('sp_Memory_Create returned no rows');

    return created.memory_id;

}

describe('sp_Memory_Create', () => {

    it('inserts an active memory with correct defaults', async () => {

        const memoryId = await createMemory({ was_evidenced: true });

        const row = await ctx.kysely.selectFrom('Memory')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .executeTakeFirstOrThrow();

        expect(row.relevance_status).toBe('active');
        expect(row.access_count).toBe(0);
        expect(row.was_observed).toBe(true);
        expect(row.was_evidenced).toBe(true);
        expect(row.was_inferred).toBe(false);

    });

    it('rejects an unknown MemoryDomain', async () => {

        await expect(
            createMemory({ domain: 'not-a-real-domain' }),
        ).rejects.toThrow();

    });

    it('rejects an unknown MemoryCategory', async () => {

        await expect(
            createMemory({ category: 'not-a-real-category' }),
        ).rejects.toThrow();

    });

});

describe('sp_Memory_Update', () => {

    it('rewrites content + flags but leaves relevance_status alone', async () => {

        const memoryId = await createMemory();

        await ctx.proc('sp_Memory_Update', {
            p_memory_id:         memoryId,
            p_content:           'edited content',
            p_domain:            'frontend',
            p_category:          'pattern',
            p_reason:            'clarified',
            p_was_inferred:      true,
            p_was_observed:      false,
            p_was_evidenced:     true,
            p_was_user_provided: false,
        });

        const row = await ctx.kysely.selectFrom('Memory')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .executeTakeFirstOrThrow();

        expect(row.content).toBe('edited content');
        expect(row.domain).toBe('frontend');
        expect(row.category).toBe('pattern');
        expect(row.was_inferred).toBe(true);
        expect(row.was_observed).toBe(false);
        expect(row.relevance_status).toBe('active');

    });

});

describe('sp_Memory_Touch', () => {

    it('bumps access_count and last_accessed_at without touching updated_at', async () => {

        const memoryId = await createMemory();

        const before = await ctx.kysely.selectFrom('Memory')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .executeTakeFirstOrThrow();

        await ctx.proc('sp_Memory_Touch', { p_memory_id: memoryId, p_agent_id: 0 });
        await ctx.proc('sp_Memory_Touch', { p_memory_id: memoryId, p_agent_id: 0 });

        const after = await ctx.kysely.selectFrom('Memory')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .executeTakeFirstOrThrow();

        expect(after.access_count).toBe(2);
        expect(after.last_accessed_at.getTime()).toBeGreaterThanOrEqual(before.last_accessed_at.getTime());
        expect(after.updated_at.getTime()).toBe(before.updated_at.getTime());

    });

});

describe('sp_Memory_SetRelevance', () => {

    it('transitions active -> needs-review and writes audit rows', async () => {

        const memoryId = await createMemory();

        await ctx.proc('sp_Memory_SetRelevance', {
            p_memory_id:            memoryId,
            p_new_relevance_status: 'needs-review',
            p_agent_id:             0,
            p_reason:               'flagged for follow-up',
        });

        const memory = await ctx.kysely.selectFrom('Memory')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .executeTakeFirstOrThrow();

        expect(memory.relevance_status).toBe('needs-review');

        const subtype = await ctx.kysely.selectFrom('Memory_StateTransition')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .executeTakeFirstOrThrow();

        const transition = await ctx.kysely.selectFrom('StateTransition')
            .selectAll()
            .where('transition_id', '=', subtype.transition_id)
            .executeTakeFirstOrThrow();

        expect(transition.state_transition_type).toBe('memory-relevance');
        expect(transition.from_status).toBe('active');
        expect(transition.to_status).toBe('needs-review');

    });

    it('rejects an unallowed transition (active -> active)', async () => {

        const memoryId = await createMemory();

        // Proc raises 'transition active -> active not allowed for memory-relevance'
        // with SQLSTATE '23514' (check_violation).
        await expect(
            ctx.proc('sp_Memory_SetRelevance', {
                p_memory_id:            memoryId,
                p_new_relevance_status: 'active',
                p_agent_id:             0,
                p_reason:               'should be rejected',
            }),
        ).rejects.toThrow(/transition active -> active not allowed for memory-relevance/);

    });

});

describe('sp_Memory_Delete + sp_Memory_Restore', () => {

    it('round-trips active -> deleted -> active', async () => {

        const memoryId = await createMemory();

        await ctx.proc('sp_Memory_Delete', {
            p_memory_id: memoryId, p_agent_id: 0, p_reason: 'cleanup',
        });

        const deleted = await ctx.kysely.selectFrom('Memory')
            .select('relevance_status')
            .where('memory_id', '=', memoryId)
            .executeTakeFirstOrThrow();

        expect(deleted.relevance_status).toBe('deleted');

        await ctx.proc('sp_Memory_Restore', {
            p_memory_id: memoryId, p_agent_id: 0, p_reason: 'changed mind',
        });

        const restored = await ctx.kysely.selectFrom('Memory')
            .select('relevance_status')
            .where('memory_id', '=', memoryId)
            .executeTakeFirstOrThrow();

        expect(restored.relevance_status).toBe('active');

        const audit = await ctx.kysely.selectFrom('Memory_StateTransition')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .execute();

        expect(audit.length).toBe(2);

    });

});

describe('sp_Memory_Relate + sp_Memory_Unrelate', () => {

    it('inserts the forward edge', async () => {

        const a = await createMemory({ content: 'A' });
        const b = await createMemory({ content: 'B' });

        await ctx.proc('sp_Memory_Relate', {
            p_memory_id:         a,
            p_related_memory_id: b,
            p_relation_verb:     'supersedes',
            p_reason:            'B is the canonical version',
        });

        const edge = await ctx.kysely.selectFrom('Related_Memory')
            .selectAll()
            .where('memory_id', '=', a)
            .where('related_memory_id', '=', b)
            .executeTakeFirstOrThrow();

        expect(edge.relation_verb).toBe('supersedes');

        await ctx.proc('sp_Memory_Unrelate', {
            p_memory_id:         a,
            p_related_memory_id: b,
        });

        const stillThere = await ctx.kysely.selectFrom('Related_Memory')
            .selectAll()
            .where('memory_id', '=', a)
            .where('related_memory_id', '=', b)
            .executeTakeFirst();

        expect(stillThere).toBeUndefined();

    });

    it('rejects a self-reference', async () => {

        const a = await createMemory();

        // Proc raises 'self-reference not allowed' with SQLSTATE '22023'.
        await expect(
            ctx.proc('sp_Memory_Relate', {
                p_memory_id:         a,
                p_related_memory_id: a,
                p_relation_verb:     'supersedes',
                p_reason:            'self',
            }),
        ).rejects.toThrow(/self-reference not allowed/);

    });

});

describe('sp_Memory_Consolidate', () => {

    it('writes supersedes edge, re-points tags + projects, and superseded the duplicate', async () => {

        const canonical = await createMemory({ content: 'canonical' });
        const duplicate = await createMemory({ content: 'duplicate' });

        const tagResult = await ctx.proc('sp_Tag_Create', {
            p_name: 'shared-tag', p_description: '', p_reason: '',
            p_provenance_id: 0, p_agent_id: 0,
        });
        const [tag] = tagResult;
        if (!tag) throw new Error('tag create failed');

        await ctx.proc('sp_Tag_Attach_Memory', { p_tag_id: tag.tag_id, p_memory_id: duplicate });

        const projectResult = await ctx.proc('sp_Project_Create', {
            p_name: 'p-cons', p_filepath: '/p', p_git_repo: '', p_main_branch: 'main',
            p_git_url: '', p_agent_id: 0,
        });
        const [project] = projectResult;
        if (!project) throw new Error('project create failed');

        await ctx.proc('sp_Memory_Attach_Project', { p_memory_id: duplicate, p_project_id: project.project_id });

        await ctx.proc('sp_Memory_Consolidate', {
            p_canonical_memory_id: canonical,
            p_duplicate_memory_id: duplicate,
            p_agent_id:            0,
            p_reason:              'same fact',
        });

        const dup = await ctx.kysely.selectFrom('Memory')
            .select('relevance_status')
            .where('memory_id', '=', duplicate)
            .executeTakeFirstOrThrow();

        expect(dup.relevance_status).toBe('superseded');

        const edge = await ctx.kysely.selectFrom('Related_Memory')
            .selectAll()
            .where('memory_id', '=', canonical)
            .where('related_memory_id', '=', duplicate)
            .executeTakeFirstOrThrow();

        expect(edge.relation_verb).toBe('supersedes');

        const movedTag = await ctx.kysely.selectFrom('Memory_Tag')
            .selectAll()
            .where('tag_id', '=', tag.tag_id)
            .where('memory_id', '=', canonical)
            .executeTakeFirst();

        expect(movedTag).toBeDefined();

        const oldTag = await ctx.kysely.selectFrom('Memory_Tag')
            .selectAll()
            .where('memory_id', '=', duplicate)
            .execute();

        expect(oldTag.length).toBe(0);

        const movedProject = await ctx.kysely.selectFrom('Project_Memory')
            .selectAll()
            .where('project_id', '=', project.project_id)
            .where('memory_id', '=', canonical)
            .executeTakeFirst();

        expect(movedProject).toBeDefined();

    });

    it('rejects self-consolidation', async () => {

        const a = await createMemory();

        // Proc raises 'self-reference not allowed' with SQLSTATE '22023'.
        await expect(
            ctx.proc('sp_Memory_Consolidate', {
                p_canonical_memory_id: a,
                p_duplicate_memory_id: a,
                p_agent_id:            0,
                p_reason:              'self',
            }),
        ).rejects.toThrow(/self-reference not allowed/);

    });

});

describe('sp_Memory_Attach_Project + sp_Memory_Detach_Project', () => {

    it('writes and removes the join row', async () => {

        const memoryId = await createMemory();

        const projectResult = await ctx.proc('sp_Project_Create', {
            p_name: 'p-attach', p_filepath: '/p', p_git_repo: '', p_main_branch: 'main',
            p_git_url: '', p_agent_id: 0,
        });
        const [project] = projectResult;
        if (!project) throw new Error('project create failed');

        await ctx.proc('sp_Memory_Attach_Project', {
            p_memory_id: memoryId, p_project_id: project.project_id,
        });

        const join = await ctx.kysely.selectFrom('Project_Memory')
            .selectAll()
            .where('project_id', '=', project.project_id)
            .where('memory_id', '=', memoryId)
            .executeTakeFirst();

        expect(join).toBeDefined();

        await ctx.proc('sp_Memory_Detach_Project', {
            p_memory_id: memoryId, p_project_id: project.project_id,
        });

        const gone = await ctx.kysely.selectFrom('Project_Memory')
            .selectAll()
            .where('project_id', '=', project.project_id)
            .where('memory_id', '=', memoryId)
            .executeTakeFirst();

        expect(gone).toBeUndefined();

    });

});

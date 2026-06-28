import { beforeAll, beforeEach, describe, it, expect } from 'bun:test';

import { bootstrap, truncateAll } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});

beforeEach(async () => {

    await truncateAll(ctx);

});

async function createAgent(overrides: Partial<{ name: string; description: string }> = {}): Promise<number> {

    const result = await ctx.proc('sp_Agent_Create', {
        p_name:        overrides.name        ?? 'Ada Lovelace',
        p_description: overrides.description ?? 'first programmer, sql tester',
    });

    const [created] = result;
    if (!created) throw new Error('sp_Agent_Create returned no rows');

    return created.agent_id;

}

describe('sp_Agent_Create', () => {

    it('inserts a new agent and returns an agent_id > 0', async () => {

        const agentId = await createAgent({
            name:        'Grace Hopper',
            description: 'compiler pioneer',
        });

        expect(agentId).toBeGreaterThan(0);

        const row = await ctx.kysely.selectFrom('Agent')
            .selectAll()
            .where('agent_id', '=', agentId)
            .executeTakeFirstOrThrow();

        expect(row.name).toBe('Grace Hopper');
        expect(row.description).toBe('compiler pioneer');

    });

    it('persists distinct names across multiple inserts', async () => {

        const a = await createAgent({ name: 'Alice',   description: 'reviewer' });
        const b = await createAgent({ name: 'Bob',     description: 'author'   });
        const c = await createAgent({ name: 'Charlie', description: 'auditor'  });

        expect(new Set([a, b, c]).size).toBe(3);

        const rows = await ctx.kysely.selectFrom('Agent')
            .select(['agent_id', 'name'])
            .where('agent_id', 'in', [a, b, c])
            .orderBy('agent_id', 'asc')
            .execute();

        expect(rows.map(r => r.name)).toEqual(['Alice', 'Bob', 'Charlie']);

    });

});

describe('sp_Agent_Update', () => {

    it('updates name + description and advances updated_at', async () => {

        const agentId = await createAgent({ name: 'before', description: 'old description' });

        const before = await ctx.kysely.selectFrom('Agent')
            .selectAll()
            .where('agent_id', '=', agentId)
            .executeTakeFirstOrThrow();

        // Wait a tick so NOW() advances meaningfully on fast machines.
        await new Promise(resolve => setTimeout(resolve, 25));

        await ctx.proc('sp_Agent_Update', {
            p_agent_id:    agentId,
            p_name:        'after',
            p_description: 'new description',
        });

        const after = await ctx.kysely.selectFrom('Agent')
            .selectAll()
            .where('agent_id', '=', agentId)
            .executeTakeFirstOrThrow();

        expect(after.name).toBe('after');
        expect(after.description).toBe('new description');
        expect(after.created_at.getTime()).toBe(before.created_at.getTime());
        expect(after.updated_at.getTime()).toBeGreaterThan(before.updated_at.getTime());

    });

    it('rejects an update against the sentinel agent_id=0', async () => {

        await expect(
            ctx.proc('sp_Agent_Update', {
                p_agent_id:    0,
                p_name:        'cannot rename sentinel',
                p_description: 'should fail',
            }),
        ).rejects.toThrow(/Sentinel agent_id=0 is immutable/);

    });

    it('is a no-op against a non-existent agent_id (no error, no row created)', async () => {

        // Pick an id that almost certainly doesn't exist after truncate.
        const ghostId = 999_999;

        await ctx.proc('sp_Agent_Update', {
            p_agent_id:    ghostId,
            p_name:        'ghost',
            p_description: 'should not appear',
        });

        const row = await ctx.kysely.selectFrom('Agent')
            .selectAll()
            .where('agent_id', '=', ghostId)
            .executeTakeFirst();

        expect(row).toBeUndefined();

    });

});

describe('sp_Agent_Delete', () => {

    it('removes the agent and reassigns owned entities to agent_id=0', async () => {

        const agentId = await createAgent({ name: 'doomed', description: 'about to be deleted' });

        // Project belongs to the agent under test.
        const projectResult = await ctx.proc('sp_Project_Create', {
            p_name:        'doomed-project',
            p_filepath:    '/tmp/doomed',
            p_git_repo:    'doomed/repo',
            p_main_branch: 'main',
            p_git_url:     'git@example.com:doomed/repo.git',
            p_agent_id:    agentId,
        });
        const [project] = projectResult;
        if (!project) throw new Error('project create failed');

        // Memory authored by the agent.
        const memoryResult = await ctx.proc('sp_Memory_Create', {
            p_content:           'agent observed this',
            p_domain:            'backend',
            p_category:          'fact',
            p_reason:            'agent-delete fixture',
            p_provenance_id:     0,
            p_agent_id:          agentId,
            p_was_inferred:      false,
            p_was_observed:      true,
            p_was_evidenced:     false,
            p_was_user_provided: false,
        });
        const [memory] = memoryResult;
        if (!memory) throw new Error('memory create failed');

        // Note authored by the agent (project subtype, since Note has a typed FK requirement).
        const noteResult = await ctx.proc('sp_Note_Create_Project', {
            p_content:       'agent wrote this',
            p_reason:        'agent-delete fixture',
            p_provenance_id: 0,
            p_agent_id:      agentId,
            p_project_id:    project.project_id,
        });
        const [note] = noteResult;
        if (!note) throw new Error('note create failed');

        await ctx.proc('sp_Agent_Delete', { p_agent_id: agentId });

        // Agent row gone.
        const deletedAgent = await ctx.kysely.selectFrom('Agent')
            .selectAll()
            .where('agent_id', '=', agentId)
            .executeTakeFirst();

        expect(deletedAgent).toBeUndefined();

        // Owned entities reassigned to sentinel (agent_id=0).
        const reassignedProject = await ctx.kysely.selectFrom('Project')
            .select('agent_id')
            .where('project_id', '=', project.project_id)
            .executeTakeFirstOrThrow();

        expect(reassignedProject.agent_id).toBe(0);

        const reassignedMemory = await ctx.kysely.selectFrom('Memory')
            .select('agent_id')
            .where('memory_id', '=', memory.memory_id)
            .executeTakeFirstOrThrow();

        expect(reassignedMemory.agent_id).toBe(0);

        const reassignedNote = await ctx.kysely.selectFrom('Note')
            .select('agent_id')
            .where('note_id', '=', note.note_id)
            .executeTakeFirstOrThrow();

        expect(reassignedNote.agent_id).toBe(0);

    });

    it('rejects deletion of the sentinel agent_id=0', async () => {

        await expect(
            ctx.proc('sp_Agent_Delete', { p_agent_id: 0 }),
        ).rejects.toThrow(/Sentinel agent_id=0 is undeletable/);

        // Sentinel row still present.
        const sentinel = await ctx.kysely.selectFrom('Agent')
            .selectAll()
            .where('agent_id', '=', 0)
            .executeTakeFirst();

        expect(sentinel).toBeDefined();

    });

});

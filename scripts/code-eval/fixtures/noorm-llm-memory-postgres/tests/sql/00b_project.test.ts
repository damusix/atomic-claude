import { beforeAll, beforeEach, describe, it, expect } from 'bun:test';

import { bootstrap, truncateAll } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});

beforeEach(async () => {

    await truncateAll(ctx);

});

type ProjectInput = {
    name:        string;
    filepath:    string;
    git_repo:    string;
    main_branch: string;
    git_url:     string;
    agent_id:    number;
};

const DEFAULT_PROJECT: ProjectInput = {
    name:        'noorm',
    filepath:    '/Users/dev/projects/noorm',
    git_repo:    'noormdev/noorm',
    main_branch: 'master',
    git_url:     'git@github.com:noormdev/noorm.git',
    agent_id:    0,
};

async function createProject(overrides: Partial<ProjectInput> = {}): Promise<number> {

    const input: ProjectInput = { ...DEFAULT_PROJECT, ...overrides };

    const result = await ctx.proc('sp_Project_Create', {
        p_name:        input.name,
        p_filepath:    input.filepath,
        p_git_repo:    input.git_repo,
        p_main_branch: input.main_branch,
        p_git_url:     input.git_url,
        p_agent_id:    input.agent_id,
    });

    const [created] = result;
    if (!created) throw new Error('sp_Project_Create returned no rows');

    return created.project_id;

}

describe('sp_Project_Create', () => {

    it('persists every input column on the resulting Project row', async () => {

        const input: ProjectInput = {
            name:        'llm-memory-db-pg',
            filepath:    '/srv/llm-memory-db-pg',
            git_repo:    'noormdev/llm-memory-db-pg',
            main_branch: 'main',
            git_url:     'git@github.com:noormdev/llm-memory-db-pg.git',
            agent_id:    0,
        };

        const projectId = await createProject(input);

        expect(projectId).toBeGreaterThan(0);

        const row = await ctx.kysely.selectFrom('Project')
            .selectAll()
            .where('project_id', '=', projectId)
            .executeTakeFirstOrThrow();

        expect(row.name).toBe(input.name);
        expect(row.filepath).toBe(input.filepath);
        expect(row.git_repo).toBe(input.git_repo);
        expect(row.main_branch).toBe(input.main_branch);
        expect(row.git_url).toBe(input.git_url);
        expect(row.agent_id).toBe(input.agent_id);
        expect(row.created_at).toBeInstanceOf(Date);
        expect(row.updated_at).toBeInstanceOf(Date);

    });

});

describe('sp_Project_Update', () => {

    it('updates name + filepath + git fields and advances updated_at', async () => {

        const projectId = await createProject({ name: 'before', filepath: '/old/path' });

        const before = await ctx.kysely.selectFrom('Project')
            .selectAll()
            .where('project_id', '=', projectId)
            .executeTakeFirstOrThrow();

        await new Promise(resolve => setTimeout(resolve, 25));

        await ctx.proc('sp_Project_Update', {
            p_project_id:  projectId,
            p_name:        'after',
            p_filepath:    '/new/path',
            p_git_repo:    'org/renamed',
            p_main_branch: 'develop',
            p_git_url:     'git@example.com:org/renamed.git',
        });

        const after = await ctx.kysely.selectFrom('Project')
            .selectAll()
            .where('project_id', '=', projectId)
            .executeTakeFirstOrThrow();

        expect(after.name).toBe('after');
        expect(after.filepath).toBe('/new/path');
        expect(after.git_repo).toBe('org/renamed');
        expect(after.main_branch).toBe('develop');
        expect(after.git_url).toBe('git@example.com:org/renamed.git');
        expect(after.created_at.getTime()).toBe(before.created_at.getTime());
        expect(after.updated_at.getTime()).toBeGreaterThan(before.updated_at.getTime());

    });

    it('rejects an update against the sentinel project_id=0', async () => {

        await expect(
            ctx.proc('sp_Project_Update', {
                p_project_id:  0,
                p_name:        'cannot rename sentinel',
                p_filepath:    '/nope',
                p_git_repo:    'nope',
                p_main_branch: 'main',
                p_git_url:     '',
            }),
        ).rejects.toThrow(/Sentinel project_id=0 is immutable/);

    });

    it('is a no-op against a non-existent project_id (no error, no row created)', async () => {

        const ghostId = 999_999;

        await ctx.proc('sp_Project_Update', {
            p_project_id:  ghostId,
            p_name:        'ghost',
            p_filepath:    '/ghost',
            p_git_repo:    'ghost/ghost',
            p_main_branch: 'main',
            p_git_url:     '',
        });

        const row = await ctx.kysely.selectFrom('Project')
            .selectAll()
            .where('project_id', '=', ghostId)
            .executeTakeFirst();

        expect(row).toBeUndefined();

    });

});

describe('sp_Project_Delete', () => {

    it('hard-deletes project, reassigns provenance to 0, and soft-deletes attached notes', async () => {

        const projectId = await createProject({ name: 'doomed-project' });

        // Memory whose provenance is the doomed project.
        const memoryResult = await ctx.proc('sp_Memory_Create', {
            p_content:           'collected during the doomed project',
            p_domain:            'backend',
            p_category:          'fact',
            p_reason:            'project-delete fixture',
            p_provenance_id:     projectId,
            p_agent_id:          0,
            p_was_inferred:      false,
            p_was_observed:      true,
            p_was_evidenced:     false,
            p_was_user_provided: false,
        });
        const [memory] = memoryResult;
        if (!memory) throw new Error('memory create failed');

        // Note attached to the doomed project via Project_Note.
        const noteResult = await ctx.proc('sp_Note_Create_Project', {
            p_content:       'doc for doomed project',
            p_reason:        'project-delete fixture',
            p_provenance_id: 0,
            p_agent_id:      0,
            p_project_id:    projectId,
        });
        const [note] = noteResult;
        if (!note) throw new Error('note create failed');

        // Sanity check: pre-conditions.
        const noteBefore = await ctx.kysely.selectFrom('Note')
            .select('relevance_status')
            .where('note_id', '=', note.note_id)
            .executeTakeFirstOrThrow();

        expect(noteBefore.relevance_status).toBe('active');

        await ctx.proc('sp_Project_Delete', { p_project_id: projectId });

        // Project row gone.
        const deletedProject = await ctx.kysely.selectFrom('Project')
            .selectAll()
            .where('project_id', '=', projectId)
            .executeTakeFirst();

        expect(deletedProject).toBeUndefined();

        // Memory provenance reassigned to sentinel project (0).
        const memoryAfter = await ctx.kysely.selectFrom('Memory')
            .select('provenance_id')
            .where('memory_id', '=', memory.memory_id)
            .executeTakeFirstOrThrow();

        expect(memoryAfter.provenance_id).toBe(0);

        // Note soft-deleted (relevance_status='deleted'), but row still exists.
        const noteAfter = await ctx.kysely.selectFrom('Note')
            .select('relevance_status')
            .where('note_id', '=', note.note_id)
            .executeTakeFirstOrThrow();

        expect(noteAfter.relevance_status).toBe('deleted');

        // The cascade removed the Project_Note join row (FK ON DELETE CASCADE).
        const projectNoteAfter = await ctx.kysely.selectFrom('Project_Note')
            .selectAll()
            .where('note_id', '=', note.note_id)
            .executeTakeFirst();

        expect(projectNoteAfter).toBeUndefined();

        // A note-relevance state-transition audit row was written for the cascade.
        const transition = await ctx.kysely.selectFrom('Note_StateTransition as ns')
            .innerJoin('StateTransition as st', 'st.transition_id', 'ns.transition_id')
            .select(['st.from_status', 'st.to_status', 'st.state_transition_type', 'st.reason', 'st.agent_id'])
            .where('ns.note_id', '=', note.note_id)
            .executeTakeFirstOrThrow();

        expect(transition.state_transition_type).toBe('note-relevance');
        expect(transition.from_status).toBe('active');
        expect(transition.to_status).toBe('deleted');
        expect(transition.agent_id).toBe(0);
        expect(transition.reason).toMatch(/Project deleted/);

    });

    it('rejects deletion of the sentinel project_id=0', async () => {

        await expect(
            ctx.proc('sp_Project_Delete', { p_project_id: 0 }),
        ).rejects.toThrow(/Sentinel project_id=0 is undeletable/);

        const sentinel = await ctx.kysely.selectFrom('Project')
            .selectAll()
            .where('project_id', '=', 0)
            .executeTakeFirst();

        expect(sentinel).toBeDefined();

    });

});

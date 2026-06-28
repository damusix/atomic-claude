import { beforeAll, beforeEach, describe, it, expect } from 'bun:test';

import { bootstrap, truncateAll } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});

beforeEach(async () => {

    await truncateAll(ctx);

});

async function makeTag(name: string): Promise<number> {

    const result = await ctx.proc('sp_Tag_Create', {
        p_name: name, p_description: '', p_reason: 'fixture',
        p_provenance_id: 0, p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('tag create failed');
    return created.tag_id;

}

async function makeMemory(): Promise<number> {

    const result = await ctx.proc('sp_Memory_Create', {
        p_content: 'm', p_domain: 'backend', p_category: 'fact',
        p_reason: 'fixture', p_provenance_id: 0, p_agent_id: 0,
        p_was_inferred: false, p_was_observed: true,
        p_was_evidenced: false, p_was_user_provided: false,
    });
    const [created] = result;
    if (!created) throw new Error('memory create failed');
    return created.memory_id;

}

async function makeArtifact(): Promise<number> {

    const result = await ctx.proc('sp_Artifact_Create', {
        p_title: 'a', p_description: '', p_filepath: 'a.md',
        p_reason: 'fixture', p_provenance_id: 0, p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('artifact create failed');
    return created.artifact_id;

}

async function makeMilestone(): Promise<number> {

    const result = await ctx.proc('sp_Milestone_Create', {
        p_title: 'M', p_content: '', p_reason: 'fixture',
        p_provenance_id: 0, p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('milestone create failed');
    return created.milestone_id;

}

async function makeTask(milestoneId: number): Promise<{ milestone_id: number; task_no: number }> {

    const result = await ctx.proc('sp_Task_Create', {
        p_milestone_id: milestoneId, p_title: 'T', p_content: '',
        p_reason: 'fixture', p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('task create failed');
    return created;

}

async function makeProject(name: string): Promise<number> {

    const result = await ctx.proc('sp_Project_Create', {
        p_name: name, p_filepath: '/p', p_git_repo: '', p_main_branch: 'main',
        p_git_url: '', p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('project create failed');
    return created.project_id;

}

describe('sp_Tag_Create', () => {

    it('inserts a Tag and returns its id', async () => {

        const id = await makeTag('alpha');

        const row = await ctx.kysely.selectFrom('Tag')
            .selectAll()
            .where('tag_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(row.name).toBe('alpha');

    });

    it('rejects a duplicate name', async () => {

        await makeTag('dup');

        // Proc raises 'Tag name already exists: dup' with SQLSTATE '23505'
        // (unique_violation).
        await expect(makeTag('dup')).rejects.toThrow(/Tag name already exists: dup/);

    });

});

describe('sp_Tag_Update', () => {

    it('rewrites name + description + reason', async () => {

        const id = await makeTag('original');

        await ctx.proc('sp_Tag_Update', {
            p_tag_id: id, p_name: 'renamed', p_description: 'd', p_reason: 'r',
        });

        const row = await ctx.kysely.selectFrom('Tag')
            .selectAll()
            .where('tag_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(row.name).toBe('renamed');
        expect(row.description).toBe('d');

    });

});

describe('sp_Tag_Delete', () => {

    it('hard-deletes the Tag row', async () => {

        const id = await makeTag('to-delete');

        await ctx.proc('sp_Tag_Delete', { p_tag_id: id });

        const row = await ctx.kysely.selectFrom('Tag')
            .selectAll()
            .where('tag_id', '=', id)
            .executeTakeFirst();

        expect(row).toBeUndefined();

    });

});

describe('sp_Tag_Attach_Project + sp_Tag_Detach_Project', () => {

    it('writes and removes the Project_Tag join row', async () => {

        const tagId = await makeTag('p-tag');
        const projectId = await makeProject('p-attach');

        await ctx.proc('sp_Tag_Attach_Project', { p_tag_id: tagId, p_project_id: projectId });

        const join = await ctx.kysely.selectFrom('Project_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .where('project_id', '=', projectId)
            .executeTakeFirst();

        expect(join).toBeDefined();

        await ctx.proc('sp_Tag_Detach_Project', { p_tag_id: tagId, p_project_id: projectId });

        const gone = await ctx.kysely.selectFrom('Project_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .executeTakeFirst();

        expect(gone).toBeUndefined();

    });

});

describe('sp_Tag_Attach_Memory + sp_Tag_Detach_Memory', () => {

    it('writes and removes the Memory_Tag join row', async () => {

        const tagId = await makeTag('m-tag');
        const memoryId = await makeMemory();

        await ctx.proc('sp_Tag_Attach_Memory', { p_tag_id: tagId, p_memory_id: memoryId });

        const join = await ctx.kysely.selectFrom('Memory_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .where('memory_id', '=', memoryId)
            .executeTakeFirst();

        expect(join).toBeDefined();

        await ctx.proc('sp_Tag_Detach_Memory', { p_tag_id: tagId, p_memory_id: memoryId });

        const gone = await ctx.kysely.selectFrom('Memory_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .executeTakeFirst();

        expect(gone).toBeUndefined();

    });

});

describe('sp_Tag_Attach_Artifact + sp_Tag_Detach_Artifact', () => {

    it('writes and removes the Artifact_Tag join row', async () => {

        const tagId = await makeTag('a-tag');
        const artifactId = await makeArtifact();

        await ctx.proc('sp_Tag_Attach_Artifact', { p_tag_id: tagId, p_artifact_id: artifactId });

        const join = await ctx.kysely.selectFrom('Artifact_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .where('artifact_id', '=', artifactId)
            .executeTakeFirst();

        expect(join).toBeDefined();

        await ctx.proc('sp_Tag_Detach_Artifact', { p_tag_id: tagId, p_artifact_id: artifactId });

        const gone = await ctx.kysely.selectFrom('Artifact_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .executeTakeFirst();

        expect(gone).toBeUndefined();

    });

});

describe('sp_Tag_Attach_Milestone + sp_Tag_Detach_Milestone', () => {

    it('writes and removes the Milestone_Tag join row', async () => {

        const tagId = await makeTag('ms-tag');
        const msId = await makeMilestone();

        await ctx.proc('sp_Tag_Attach_Milestone', { p_tag_id: tagId, p_milestone_id: msId });

        const join = await ctx.kysely.selectFrom('Milestone_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .where('milestone_id', '=', msId)
            .executeTakeFirst();

        expect(join).toBeDefined();

        await ctx.proc('sp_Tag_Detach_Milestone', { p_tag_id: tagId, p_milestone_id: msId });

        const gone = await ctx.kysely.selectFrom('Milestone_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .executeTakeFirst();

        expect(gone).toBeUndefined();

    });

});

describe('sp_Tag_Attach_Task + sp_Tag_Detach_Task', () => {

    it('writes and removes the Task_Tag join row', async () => {

        const tagId = await makeTag('t-tag');
        const msId = await makeMilestone();
        const task = await makeTask(msId);

        await ctx.proc('sp_Tag_Attach_Task', {
            p_tag_id: tagId, p_milestone_id: task.milestone_id, p_task_no: task.task_no,
        });

        const join = await ctx.kysely.selectFrom('Task_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .where('milestone_id', '=', task.milestone_id)
            .where('task_no', '=', task.task_no)
            .executeTakeFirst();

        expect(join).toBeDefined();

        await ctx.proc('sp_Tag_Detach_Task', {
            p_tag_id: tagId, p_milestone_id: task.milestone_id, p_task_no: task.task_no,
        });

        const gone = await ctx.kysely.selectFrom('Task_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .executeTakeFirst();

        expect(gone).toBeUndefined();

    });

});

describe('sp_Tag_Merge', () => {

    it('re-points all five *_Tag tables and hard-deletes the source tag', async () => {

        const sourceId = await makeTag('source');
        const targetId = await makeTag('target');

        const projectId = await makeProject('p-merge');
        const memoryId  = await makeMemory();
        const artifactId = await makeArtifact();
        const msId = await makeMilestone();
        const task = await makeTask(msId);

        await ctx.proc('sp_Tag_Attach_Project',   { p_tag_id: sourceId, p_project_id: projectId });
        await ctx.proc('sp_Tag_Attach_Memory',    { p_tag_id: sourceId, p_memory_id: memoryId });
        await ctx.proc('sp_Tag_Attach_Artifact',  { p_tag_id: sourceId, p_artifact_id: artifactId });
        await ctx.proc('sp_Tag_Attach_Milestone', { p_tag_id: sourceId, p_milestone_id: msId });
        await ctx.proc('sp_Tag_Attach_Task',      {
            p_tag_id: sourceId, p_milestone_id: task.milestone_id, p_task_no: task.task_no,
        });

        await ctx.proc('sp_Tag_Merge', {
            p_source_tag_id: sourceId, p_target_tag_id: targetId,
            p_agent_id: 0, p_reason: 'duplicate',
        });

        const sourceTag = await ctx.kysely.selectFrom('Tag')
            .selectAll()
            .where('tag_id', '=', sourceId)
            .executeTakeFirst();

        expect(sourceTag).toBeUndefined();

        const movedProject = await ctx.kysely.selectFrom('Project_Tag')
            .selectAll()
            .where('tag_id', '=', targetId)
            .where('project_id', '=', projectId)
            .executeTakeFirst();
        expect(movedProject).toBeDefined();

        const movedMemory = await ctx.kysely.selectFrom('Memory_Tag')
            .selectAll()
            .where('tag_id', '=', targetId)
            .where('memory_id', '=', memoryId)
            .executeTakeFirst();
        expect(movedMemory).toBeDefined();

        const movedArtifact = await ctx.kysely.selectFrom('Artifact_Tag')
            .selectAll()
            .where('tag_id', '=', targetId)
            .where('artifact_id', '=', artifactId)
            .executeTakeFirst();
        expect(movedArtifact).toBeDefined();

        const movedMs = await ctx.kysely.selectFrom('Milestone_Tag')
            .selectAll()
            .where('tag_id', '=', targetId)
            .where('milestone_id', '=', msId)
            .executeTakeFirst();
        expect(movedMs).toBeDefined();

        const movedTask = await ctx.kysely.selectFrom('Task_Tag')
            .selectAll()
            .where('tag_id', '=', targetId)
            .where('milestone_id', '=', task.milestone_id)
            .where('task_no', '=', task.task_no)
            .executeTakeFirst();
        expect(movedTask).toBeDefined();

    });

    it('rejects merging a tag into itself', async () => {

        const id = await makeTag('self-merge');

        // Proc raises 'cannot merge tag into itself' with SQLSTATE '22023'.
        await expect(
            ctx.proc('sp_Tag_Merge', {
                p_source_tag_id: id, p_target_tag_id: id,
                p_agent_id: 0, p_reason: 'self',
            }),
        ).rejects.toThrow(/cannot merge tag into itself/);

    });

});

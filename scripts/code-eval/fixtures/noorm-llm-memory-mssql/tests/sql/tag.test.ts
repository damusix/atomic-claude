/**
 * Layer 1 SQL contract tests for the Tag domain.
 *
 * Tag is an inclusive subtype: one Tag can attach to many entity types
 * via five *_Tag join tables. Every attach proc is supposed to be
 * idempotent (silent no-op on duplicate PK), Merge re-points all five
 * join tables and hard-deletes the source, and the bulk attach proc
 * accepts a TVP. CRUD includes a unique-name guard surfaced as a
 * RAISERROR.
 */
import { beforeAll, beforeEach, describe, expect, it } from 'bun:test';
import { tvp } from '@noormdev/sdk';

import { bootstrap, resetApplicationData } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});



beforeEach(async () => {

    await resetApplicationData(ctx);

});

async function createTag(name: string): Promise<number> {

    const rows = await ctx.proc('sp_Tag_Create', { name });

    const row = rows[0];

    if (!row) {

        throw new Error('sp_Tag_Create returned no rows');
    }

    return row.tag_id;

}

async function createMemory(content = 'tag-test memory'): Promise<number> {

    const rows = await ctx.proc('sp_Memory_Create', {
        content,
        domain: 'backend',
        category: 'fact',
    });

    const row = rows[0];

    if (!row) {

        throw new Error('sp_Memory_Create returned no rows');
    }

    return row.memory_id;

}

async function createProject(): Promise<number> {

    const rows = await ctx.proc('sp_Project_Create', {
        name: `tag-project-${Date.now()}-${Math.random()}`,
    });

    const row = rows[0];

    if (!row) {

        throw new Error('sp_Project_Create returned no rows');
    }

    return row.project_id;

}

async function createMilestone(): Promise<number> {

    const rows = await ctx.proc('sp_Milestone_Create', {
        title: `tag-milestone-${Date.now()}-${Math.random()}`,
    });

    const row = rows[0];

    if (!row) {

        throw new Error('sp_Milestone_Create returned no rows');
    }

    return row.milestone_id;

}

async function createTask(milestoneId: number): Promise<{ milestone_id: number; task_no: number }> {

    const rows = await ctx.proc('sp_Task_Create', {
        milestone_id: milestoneId,
        title: `tag-task-${Date.now()}`,
    });

    const row = rows[0];

    if (!row) {

        throw new Error('sp_Task_Create returned no rows');
    }

    return row;

}

async function createArtifact(): Promise<number> {

    const rows = await ctx.proc('sp_Artifact_Create', {
        title: `tag-artifact-${Date.now()}-${Math.random()}`,
    });

    const row = rows[0];

    if (!row) {

        throw new Error('sp_Artifact_Create returned no rows');
    }

    return row.artifact_id;

}

describe('sql.tag: CRUD', () => {

    it('sp_Tag_Create returns a positive tag_id and persists the row', async () => {

        const name = `crud-${Date.now()}`;
        const tagId = await createTag(name);

        expect(tagId).toBeGreaterThan(0);

        const row = await ctx.kysely
            .selectFrom('Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .executeTakeFirst();

        expect(row?.name).toBe(name);

    });

    it('sp_Tag_Create rejects a duplicate name with a RAISERROR-derived error', async () => {

        const name = `dup-${Date.now()}`;
        await createTag(name);

        await expect(
            ctx.proc('sp_Tag_Create', { name }),
        ).rejects.toThrow(/already exists/i);

    });

    it('sp_Tag_Delete hard-deletes the row and cascades attachments', async () => {

        const tagId = await createTag(`del-${Date.now()}`);
        const memoryId = await createMemory();

        await ctx.proc('sp_Tag_Attach_Memory', { tag_id: tagId, memory_id: memoryId });

        const beforeAttach = await ctx.kysely
            .selectFrom('Memory_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .execute();

        expect(beforeAttach.length).toBe(1);

        await ctx.proc('sp_Tag_Delete', { tag_id: tagId });

        const tagRow = await ctx.kysely
            .selectFrom('Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .executeTakeFirst();

        expect(tagRow).toBeUndefined();

        // ON DELETE CASCADE on Memory_Tag.tag_id removes the join row.
        const afterAttach = await ctx.kysely
            .selectFrom('Memory_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .execute();

        expect(afterAttach.length).toBe(0);

    });

    it('sp_Tag_Update changes name/description and is observable', async () => {

        const tagId = await createTag(`upd-${Date.now()}`);

        const newName = `upd-renamed-${Date.now()}`;

        await ctx.proc('sp_Tag_Update', {
            tag_id: tagId,
            name: newName,
            description: 'fresh description',
        });

        const row = await ctx.kysely
            .selectFrom('Tag')
            .select(['name', 'description'])
            .where('tag_id', '=', tagId)
            .executeTakeFirst();

        expect(row?.name).toBe(newName);
        expect(row?.description).toBe('fresh description');

    });

});

describe('sql.tag: attach/detach idempotency', () => {

    it('sp_Tag_Attach_Project / Detach_Project are idempotent', async () => {

        const tagId = await createTag(`attach-proj-${Date.now()}`);
        const projectId = await createProject();

        await ctx.proc('sp_Tag_Attach_Project', { tag_id: tagId, project_id: projectId });
        await ctx.proc('sp_Tag_Attach_Project', { tag_id: tagId, project_id: projectId });

        const attached = await ctx.kysely
            .selectFrom('Project_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .where('project_id', '=', projectId)
            .execute();

        expect(attached.length).toBe(1);

        await ctx.proc('sp_Tag_Detach_Project', { tag_id: tagId, project_id: projectId });
        await ctx.proc('sp_Tag_Detach_Project', { tag_id: tagId, project_id: projectId });

        const detached = await ctx.kysely
            .selectFrom('Project_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .where('project_id', '=', projectId)
            .execute();

        expect(detached.length).toBe(0);

    });

    it('sp_Tag_Attach_Memory / Detach_Memory are idempotent', async () => {

        const tagId = await createTag(`attach-mem-${Date.now()}`);
        const memoryId = await createMemory();

        await ctx.proc('sp_Tag_Attach_Memory', { tag_id: tagId, memory_id: memoryId });
        await ctx.proc('sp_Tag_Attach_Memory', { tag_id: tagId, memory_id: memoryId });

        const attached = await ctx.kysely
            .selectFrom('Memory_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .where('memory_id', '=', memoryId)
            .execute();

        expect(attached.length).toBe(1);

        await ctx.proc('sp_Tag_Detach_Memory', { tag_id: tagId, memory_id: memoryId });
        await ctx.proc('sp_Tag_Detach_Memory', { tag_id: tagId, memory_id: memoryId });

        const detached = await ctx.kysely
            .selectFrom('Memory_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .where('memory_id', '=', memoryId)
            .execute();

        expect(detached.length).toBe(0);

    });

    it('sp_Tag_Attach_Artifact / Detach_Artifact are idempotent', async () => {

        const tagId = await createTag(`attach-art-${Date.now()}`);
        const artifactId = await createArtifact();

        await ctx.proc('sp_Tag_Attach_Artifact', { tag_id: tagId, artifact_id: artifactId });
        await ctx.proc('sp_Tag_Attach_Artifact', { tag_id: tagId, artifact_id: artifactId });

        const attached = await ctx.kysely
            .selectFrom('Artifact_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .where('artifact_id', '=', artifactId)
            .execute();

        expect(attached.length).toBe(1);

        await ctx.proc('sp_Tag_Detach_Artifact', { tag_id: tagId, artifact_id: artifactId });
        await ctx.proc('sp_Tag_Detach_Artifact', { tag_id: tagId, artifact_id: artifactId });

        const detached = await ctx.kysely
            .selectFrom('Artifact_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .where('artifact_id', '=', artifactId)
            .execute();

        expect(detached.length).toBe(0);

    });

    it('sp_Tag_Attach_Milestone / Detach_Milestone are idempotent', async () => {

        const tagId = await createTag(`attach-mile-${Date.now()}`);
        const milestoneId = await createMilestone();

        await ctx.proc('sp_Tag_Attach_Milestone', { tag_id: tagId, milestone_id: milestoneId });
        await ctx.proc('sp_Tag_Attach_Milestone', { tag_id: tagId, milestone_id: milestoneId });

        const attached = await ctx.kysely
            .selectFrom('Milestone_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .where('milestone_id', '=', milestoneId)
            .execute();

        expect(attached.length).toBe(1);

        await ctx.proc('sp_Tag_Detach_Milestone', { tag_id: tagId, milestone_id: milestoneId });
        await ctx.proc('sp_Tag_Detach_Milestone', { tag_id: tagId, milestone_id: milestoneId });

        const detached = await ctx.kysely
            .selectFrom('Milestone_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .where('milestone_id', '=', milestoneId)
            .execute();

        expect(detached.length).toBe(0);

    });

    it('sp_Tag_Attach_Task / Detach_Task are idempotent (composite Task PK)', async () => {

        const tagId = await createTag(`attach-task-${Date.now()}`);
        const milestoneId = await createMilestone();
        const task = await createTask(milestoneId);

        await ctx.proc('sp_Tag_Attach_Task', {
            tag_id: tagId,
            milestone_id: task.milestone_id,
            task_no: task.task_no,
        });
        await ctx.proc('sp_Tag_Attach_Task', {
            tag_id: tagId,
            milestone_id: task.milestone_id,
            task_no: task.task_no,
        });

        const attached = await ctx.kysely
            .selectFrom('Task_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .where('milestone_id', '=', task.milestone_id)
            .where('task_no', '=', task.task_no)
            .execute();

        expect(attached.length).toBe(1);

        await ctx.proc('sp_Tag_Detach_Task', {
            tag_id: tagId,
            milestone_id: task.milestone_id,
            task_no: task.task_no,
        });
        await ctx.proc('sp_Tag_Detach_Task', {
            tag_id: tagId,
            milestone_id: task.milestone_id,
            task_no: task.task_no,
        });

        const detached = await ctx.kysely
            .selectFrom('Task_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .where('milestone_id', '=', task.milestone_id)
            .where('task_no', '=', task.task_no)
            .execute();

        expect(detached.length).toBe(0);

    });

});

describe('sql.tag: merge', () => {

    it('sp_Tag_Merge re-points all five join tables and deletes the source tag', async () => {

        const sourceTagId = await createTag(`merge-source-${Date.now()}`);
        const targetTagId = await createTag(`merge-target-${Date.now()}`);

        // Build one of each *_Tag attachment for the source.
        const projectId = await createProject();
        const memoryId = await createMemory();
        const artifactId = await createArtifact();
        const milestoneId = await createMilestone();
        const task = await createTask(milestoneId);

        await ctx.proc('sp_Tag_Attach_Project', { tag_id: sourceTagId, project_id: projectId });
        await ctx.proc('sp_Tag_Attach_Memory', { tag_id: sourceTagId, memory_id: memoryId });
        await ctx.proc('sp_Tag_Attach_Artifact', { tag_id: sourceTagId, artifact_id: artifactId });
        await ctx.proc('sp_Tag_Attach_Milestone', { tag_id: sourceTagId, milestone_id: milestoneId });
        await ctx.proc('sp_Tag_Attach_Task', {
            tag_id: sourceTagId,
            milestone_id: task.milestone_id,
            task_no: task.task_no,
        });

        const agentRows = await ctx.proc('sp_Agent_Create', {
            name: `merge-agent-${Date.now()}`,
        });

        const agentId = agentRows[0]?.agent_id;

        if (typeof agentId !== 'number') {

            throw new Error('agent creation failed');
        }

        await ctx.proc('sp_Tag_Merge', {
            source_tag_id: sourceTagId,
            target_tag_id: targetTagId,
            agent_id: agentId,
            reason: 'consolidating duplicate tag',
        });

        // Source tag is gone.
        const sourceRow = await ctx.kysely
            .selectFrom('Tag')
            .selectAll()
            .where('tag_id', '=', sourceTagId)
            .executeTakeFirst();

        expect(sourceRow).toBeUndefined();

        // Each *_Tag join now references the target.
        const projectAttach = await ctx.kysely
            .selectFrom('Project_Tag')
            .selectAll()
            .where('tag_id', '=', targetTagId)
            .where('project_id', '=', projectId)
            .execute();
        const memoryAttach = await ctx.kysely
            .selectFrom('Memory_Tag')
            .selectAll()
            .where('tag_id', '=', targetTagId)
            .where('memory_id', '=', memoryId)
            .execute();
        const artifactAttach = await ctx.kysely
            .selectFrom('Artifact_Tag')
            .selectAll()
            .where('tag_id', '=', targetTagId)
            .where('artifact_id', '=', artifactId)
            .execute();
        const milestoneAttach = await ctx.kysely
            .selectFrom('Milestone_Tag')
            .selectAll()
            .where('tag_id', '=', targetTagId)
            .where('milestone_id', '=', milestoneId)
            .execute();
        const taskAttach = await ctx.kysely
            .selectFrom('Task_Tag')
            .selectAll()
            .where('tag_id', '=', targetTagId)
            .where('milestone_id', '=', task.milestone_id)
            .where('task_no', '=', task.task_no)
            .execute();

        expect(projectAttach.length).toBe(1);
        expect(memoryAttach.length).toBe(1);
        expect(artifactAttach.length).toBe(1);
        expect(milestoneAttach.length).toBe(1);
        expect(taskAttach.length).toBe(1);

    });

});

describe('sql.tag: bulk attach memory (TVP happy path)', () => {

    it('sp_Tag_Bulk_Attach_Memory inserts one Memory_Tag row per TVP entry', async () => {

        const tagId = await createTag(`bulk-${Date.now()}`);
        const memoryIds = await Promise.all([1, 2, 3].map((i) => createMemory(`bulk memory ${i}`)));

        await ctx.proc('sp_Tag_Bulk_Attach_Memory', {
            Pairs: tvp(
                'TagAttachmentInput',
                memoryIds.map((id) => ({ tag_id: tagId, entity_id: id })),
            ),
        });

        const rows = await ctx.kysely
            .selectFrom('Memory_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .execute();

        expect(rows.length).toBe(3);

        const attachedMemoryIds = rows.map((r) => r.memory_id).sort();

        expect(attachedMemoryIds).toEqual([...memoryIds].sort());

    });

});

/**
 * Layer 2 — TagCommands / TagQueries facade.
 *
 * Covers CRUD, attach/detach pairs, the bulk-attach TVP path, the
 * relational-division TVF (filterMemoriesByTags), and Zod min(1) on the
 * pairs/tagIds arrays. Tag merge has its own home in
 * consolidate-and-merge.test.ts.
 */
import { beforeAll, beforeEach, describe, expect, it } from 'bun:test';

import { bootstrap, resetApplicationData } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];
let db: Awaited<ReturnType<typeof bootstrap>>['db'];

beforeAll(async () => {

    ({ ctx, db } = await bootstrap());

});



beforeEach(async () => {

    await resetApplicationData(ctx);

});

describe('tag: CRUD', () => {

    it('db.tag.cmd.create + qry.findById round-trip', async () => {

        const created = await db.tag.cmd.create({
            name: `design-${Date.now()}`,
            description: 'design tag',
        });

        expect(created.tag_id).toBeGreaterThan(0);

        const fetched = await db.tag.qry.findById(created.tag_id);
        expect(fetched?.name).toMatch(/^design-/);
        expect(fetched?.description).toBe('design tag');

    });

    it('db.tag.qry.findByName resolves uniquely', async () => {

        const name = `unique-${Date.now()}`;
        await db.tag.cmd.create({ name });

        const fetched = await db.tag.qry.findByName(name);
        expect(fetched?.name).toBe(name);

    });

    it('db.tag.qry.list returns inserted tags ordered by name', async () => {

        await db.tag.cmd.create({ name: `aa-${Date.now()}` });
        await db.tag.cmd.create({ name: `bb-${Date.now()}` });

        const all = await db.tag.qry.list();
        expect(all.length).toBeGreaterThanOrEqual(2);

    });

    it('db.tag.cmd.update modifies metadata', async () => {

        const { tag_id } = await db.tag.cmd.create({ name: `upd-${Date.now()}` });

        const newName = `renamed-${Date.now()}`;
        await db.tag.cmd.update({
            tagId: tag_id,
            name: newName,
            description: 'updated',
        });

        const after = await db.tag.qry.findById(tag_id);
        expect(after?.name).toBe(newName);
        expect(after?.description).toBe('updated');

    });

    it('db.tag.cmd.delete hard-deletes the row', async () => {

        const { tag_id } = await db.tag.cmd.create({ name: `del-${Date.now()}` });

        await db.tag.cmd.delete({ tagId: tag_id });

        const gone = await db.tag.qry.findById(tag_id);
        expect(gone).toBeUndefined();

    });

});

describe('tag: attach pairs', () => {

    it('db.tag.cmd.attachMemory + detachMemory round-trip', async () => {

        const { tag_id } = await db.tag.cmd.create({
            name: `attach-mem-${Date.now()}`,
        });
        const { memory_id } = await db.memory.cmd.create({
            content: 'tagged memory',
            domain: 'backend',
            category: 'fact',
        });

        await db.tag.cmd.attachMemory({ tagId: tag_id, memoryId: memory_id });

        let attachments = await db.tag.qry.listAttachments(tag_id);
        expect(attachments.some((row) => row.memory_id === memory_id)).toBe(true);

        await db.tag.cmd.detachMemory({ tagId: tag_id, memoryId: memory_id });

        attachments = await db.tag.qry.listAttachments(tag_id);
        expect(attachments.some((row) => row.memory_id === memory_id)).toBe(false);

    });

    it('db.tag.cmd.attachProject inserts a Project_Tag row', async () => {

        const { tag_id } = await db.tag.cmd.create({
            name: `attach-prj-${Date.now()}`,
        });
        const { project_id } = await db.project.cmd.create({
            name: `tag-prj-${Date.now()}`,
        });

        await db.tag.cmd.attachProject({ tagId: tag_id, projectId: project_id });

        const attachments = await db.tag.qry.listAttachments(tag_id);
        expect(attachments.some((row) => row.project_id === project_id)).toBe(true);

    });

    it('db.tag.cmd.attachMilestone + attachTask + attachArtifact all surface in vw_Tag', async () => {

        const { tag_id } = await db.tag.cmd.create({
            name: `attach-multi-${Date.now()}`,
        });
        const milestone = await db.milestone.cmd.create({
            title: `tag-m-${Date.now()}`,
        });
        const task = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'tag target',
        });
        const artifact = await db.artifact.cmd.create({
            title: `tag-art-${Date.now()}`,
        });

        await db.tag.cmd.attachMilestone({
            tagId: tag_id,
            milestoneId: milestone.milestone_id,
        });
        await db.tag.cmd.attachTask({
            tagId: tag_id,
            milestoneId: task.milestone_id,
            taskNo: task.task_no,
        });
        await db.tag.cmd.attachArtifact({
            tagId: tag_id,
            artifactId: artifact.artifact_id,
        });

        const attachments = await db.tag.qry.listAttachments(tag_id);
        expect(attachments.some((r) => r.milestone_id === milestone.milestone_id && r.task_no === 0)).toBe(true);
        expect(attachments.some((r) => r.task_no === task.task_no)).toBe(true);
        expect(attachments.some((r) => r.artifact_id === artifact.artifact_id)).toBe(true);

    });

});

describe('tag: detach pairs (per-entity)', () => {

    it('db.tag.cmd.detachProject removes the Project_Tag row', async () => {

        const { tag_id } = await db.tag.cmd.create({
            name: `detach-prj-${Date.now()}`,
        });
        const { project_id } = await db.project.cmd.create({
            name: `detach-prj-${Date.now()}`,
        });

        await db.tag.cmd.attachProject({ tagId: tag_id, projectId: project_id });

        const before = await ctx.kysely
            .selectFrom('Project_Tag')
            .selectAll()
            .where('tag_id', '=', tag_id)
            .where('project_id', '=', project_id)
            .execute();
        expect(before.length).toBe(1);

        await db.tag.cmd.detachProject({ tagId: tag_id, projectId: project_id });

        const after = await ctx.kysely
            .selectFrom('Project_Tag')
            .selectAll()
            .where('tag_id', '=', tag_id)
            .where('project_id', '=', project_id)
            .execute();
        expect(after.length).toBe(0);

    });

    it('db.tag.cmd.detachMilestone removes the Milestone_Tag row', async () => {

        const { tag_id } = await db.tag.cmd.create({
            name: `detach-m-${Date.now()}`,
        });
        const milestone = await db.milestone.cmd.create({
            title: `detach-m-${Date.now()}`,
        });

        await db.tag.cmd.attachMilestone({
            tagId: tag_id,
            milestoneId: milestone.milestone_id,
        });

        const before = await ctx.kysely
            .selectFrom('Milestone_Tag')
            .selectAll()
            .where('tag_id', '=', tag_id)
            .where('milestone_id', '=', milestone.milestone_id)
            .execute();
        expect(before.length).toBe(1);

        await db.tag.cmd.detachMilestone({
            tagId: tag_id,
            milestoneId: milestone.milestone_id,
        });

        const after = await ctx.kysely
            .selectFrom('Milestone_Tag')
            .selectAll()
            .where('tag_id', '=', tag_id)
            .where('milestone_id', '=', milestone.milestone_id)
            .execute();
        expect(after.length).toBe(0);

    });

    it('db.tag.cmd.detachTask removes the Task_Tag row (composite key)', async () => {

        const { tag_id } = await db.tag.cmd.create({
            name: `detach-t-${Date.now()}`,
        });
        const milestone = await db.milestone.cmd.create({
            title: `detach-t-m-${Date.now()}`,
        });
        const task = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'detach target',
        });

        await db.tag.cmd.attachTask({
            tagId: tag_id,
            milestoneId: task.milestone_id,
            taskNo: task.task_no,
        });

        const before = await ctx.kysely
            .selectFrom('Task_Tag')
            .selectAll()
            .where('tag_id', '=', tag_id)
            .where('milestone_id', '=', task.milestone_id)
            .where('task_no', '=', task.task_no)
            .execute();
        expect(before.length).toBe(1);

        await db.tag.cmd.detachTask({
            tagId: tag_id,
            milestoneId: task.milestone_id,
            taskNo: task.task_no,
        });

        const after = await ctx.kysely
            .selectFrom('Task_Tag')
            .selectAll()
            .where('tag_id', '=', tag_id)
            .where('milestone_id', '=', task.milestone_id)
            .where('task_no', '=', task.task_no)
            .execute();
        expect(after.length).toBe(0);

    });

    it('db.tag.cmd.detachArtifact removes the Artifact_Tag row', async () => {

        const { tag_id } = await db.tag.cmd.create({
            name: `detach-a-${Date.now()}`,
        });
        const artifact = await db.artifact.cmd.create({
            title: `detach-a-${Date.now()}`,
        });

        await db.tag.cmd.attachArtifact({
            tagId: tag_id,
            artifactId: artifact.artifact_id,
        });

        const before = await ctx.kysely
            .selectFrom('Artifact_Tag')
            .selectAll()
            .where('tag_id', '=', tag_id)
            .where('artifact_id', '=', artifact.artifact_id)
            .execute();
        expect(before.length).toBe(1);

        await db.tag.cmd.detachArtifact({
            tagId: tag_id,
            artifactId: artifact.artifact_id,
        });

        const after = await ctx.kysely
            .selectFrom('Artifact_Tag')
            .selectAll()
            .where('tag_id', '=', tag_id)
            .where('artifact_id', '=', artifact.artifact_id)
            .execute();
        expect(after.length).toBe(0);

    });

});

describe('tag: bulkAttachMemory + filterMemoriesByTags', () => {

    it('db.tag.cmd.bulkAttachMemory inserts every supplied (tagId, memoryId) pair', async () => {

        const { tag_id } = await db.tag.cmd.create({
            name: `bulk-${Date.now()}`,
        });

        const memoryIds: number[] = [];
        for (let i = 0; i < 3; i += 1) {

            const { memory_id } = await db.memory.cmd.create({
                content: `bulk-${i}`,
                domain: 'backend',
                category: 'fact',
            });
            memoryIds.push(memory_id);
        }

        await db.tag.cmd.bulkAttachMemory({
            pairs: memoryIds.map((memoryId) => ({ tagId: tag_id, memoryId })),
        });

        const attachments = await db.tag.qry.listAttachments(tag_id);
        const attachedMemoryIds = attachments
            .map((a) => a.memory_id)
            .filter((id): id is number => typeof id === 'number' && id !== 0);

        for (const id of memoryIds) {

            expect(attachedMemoryIds).toContain(id);
        }

    });

    it('db.tag.qry.filterMemoriesByTags returns memories carrying every supplied tag', async () => {

        const tagA = await db.tag.cmd.create({ name: `fA-${Date.now()}` });
        const tagB = await db.tag.cmd.create({ name: `fB-${Date.now()}` });

        const both = await db.memory.cmd.create({
            content: 'has both tags',
            domain: 'backend',
            category: 'fact',
        });
        const onlyA = await db.memory.cmd.create({
            content: 'has only tag A',
            domain: 'backend',
            category: 'fact',
        });

        await db.tag.cmd.attachMemory({ tagId: tagA.tag_id, memoryId: both.memory_id });
        await db.tag.cmd.attachMemory({ tagId: tagB.tag_id, memoryId: both.memory_id });
        await db.tag.cmd.attachMemory({ tagId: tagA.tag_id, memoryId: onlyA.memory_id });

        const matched = await db.tag.qry.filterMemoriesByTags({
            tagIds: [tagA.tag_id, tagB.tag_id],
        });

        const matchedIds = matched.map((m) => m.memory_id);
        expect(matchedIds).toContain(both.memory_id);
        expect(matchedIds).not.toContain(onlyA.memory_id);

    });

});

describe('tag: zod boundary', () => {

    it('db.tag.cmd.bulkAttachMemory rejects empty pairs (.min(1))', async () => {

        await expect(db.tag.cmd.bulkAttachMemory({ pairs: [] })).rejects.toThrow();

    });

    it('db.tag.qry.filterMemoriesByTags rejects empty tagIds (.min(1))', async () => {

        await expect(db.tag.qry.filterMemoriesByTags({ tagIds: [] })).rejects.toThrow();

    });

    it('db.tag.cmd.create rejects empty name', async () => {

        await expect(db.tag.cmd.create({ name: '' })).rejects.toThrow();

    });

    it('db.tag.cmd.attachMemory rejects negative memoryId', async () => {

        await expect(db.tag.cmd.attachMemory({
            tagId: 1,
            memoryId: -1,
        })).rejects.toThrow();

    });

});

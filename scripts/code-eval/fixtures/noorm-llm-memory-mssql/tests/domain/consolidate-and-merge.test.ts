/**
 * Layer 2 — cross-domain merge round trips through the facade.
 *
 * sp_Memory_Consolidate re-points Memory_Tag + Project_Memory rows from
 * the duplicate to the canonical, then drives the duplicate to
 * 'superseded' via SetRelevance. sp_Tag_Merge does the same dance across
 * all 5 *_Tag join tables (Project_Tag, Memory_Tag, Artifact_Tag,
 * Milestone_Tag, Task_Tag), then hard-deletes the source tag.
 *
 * These tests verify every join is re-pointed and the source row's final
 * status reflects the merge.
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

describe('memory.consolidate', () => {

    it('db.memory.cmd.consolidate marks duplicate superseded and re-points tags + projects', async () => {

        const agent = await db.agent.cmd.create({
            name: `consolidate-${Date.now()}`,
        });

        const canonical = await db.memory.cmd.create({
            content: 'canonical fact',
            domain: 'backend',
            category: 'fact',
        });
        const duplicate = await db.memory.cmd.create({
            content: 'duplicate fact',
            domain: 'backend',
            category: 'fact',
        });

        const tag = await db.tag.cmd.create({
            name: `consolidate-tag-${Date.now()}`,
        });
        const project = await db.project.cmd.create({
            name: `consolidate-prj-${Date.now()}`,
        });

        // Attach tag + project ONLY to the duplicate. After consolidation
        // they should belong to the canonical and the duplicate should be
        // gone from those join tables.
        await db.tag.cmd.attachMemory({
            tagId: tag.tag_id,
            memoryId: duplicate.memory_id,
        });
        await db.memory.cmd.attachProject({
            memoryId: duplicate.memory_id,
            projectId: project.project_id,
        });

        await db.memory.cmd.consolidate({
            canonicalMemoryId: canonical.memory_id,
            duplicateMemoryId: duplicate.memory_id,
            agentId: agent.agent_id,
            reason: 'merge',
        });

        // 1. Duplicate now superseded.
        const dup = await db.memory.qry.findById(duplicate.memory_id);
        expect(dup?.relevance_status).toBe('superseded');

        const can = await db.memory.qry.findById(canonical.memory_id);
        expect(can?.relevance_status).toBe('active');

        // 2. Memory_Tag re-pointed.
        const memTag = await ctx.kysely
            .selectFrom('Memory_Tag')
            .selectAll()
            .where('tag_id', '=', tag.tag_id)
            .execute();
        expect(memTag.length).toBe(1);
        expect(memTag[0]?.memory_id).toBe(canonical.memory_id);

        // 3. Project_Memory re-pointed.
        const projMem = await ctx.kysely
            .selectFrom('Project_Memory')
            .selectAll()
            .where('project_id', '=', project.project_id)
            .execute();
        expect(projMem.length).toBe(1);
        expect(projMem[0]?.memory_id).toBe(canonical.memory_id);

        // 4. The "supersedes" relation was recorded.
        const related = await db.memory.qry.listRelated(canonical.memory_id);
        expect(related.some((r) => r.related_memory_id === duplicate.memory_id)).toBe(true);

    });

    it('db.memory.cmd.consolidate skips re-points already on canonical (no PK conflict)', async () => {

        const agent = await db.agent.cmd.create({
            name: `consolidate-skip-${Date.now()}`,
        });

        const canonical = await db.memory.cmd.create({
            content: 'canonical', domain: 'backend', category: 'fact',
        });
        const duplicate = await db.memory.cmd.create({
            content: 'duplicate', domain: 'backend', category: 'fact',
        });
        const tag = await db.tag.cmd.create({
            name: `skip-tag-${Date.now()}`,
        });

        await db.tag.cmd.attachMemory({ tagId: tag.tag_id, memoryId: canonical.memory_id });
        await db.tag.cmd.attachMemory({ tagId: tag.tag_id, memoryId: duplicate.memory_id });

        await db.memory.cmd.consolidate({
            canonicalMemoryId: canonical.memory_id,
            duplicateMemoryId: duplicate.memory_id,
            agentId: agent.agent_id,
        });

        // Single row remains pointing at canonical, no duplicate-PK error.
        const memTag = await ctx.kysely
            .selectFrom('Memory_Tag')
            .selectAll()
            .where('tag_id', '=', tag.tag_id)
            .execute();
        expect(memTag.length).toBe(1);
        expect(memTag[0]?.memory_id).toBe(canonical.memory_id);

    });

});

describe('tag.merge', () => {

    it('db.tag.cmd.merge re-points all 5 join tables and hard-deletes the source', async () => {

        const agent = await db.agent.cmd.create({
            name: `merge-${Date.now()}`,
        });

        const source = await db.tag.cmd.create({
            name: `merge-src-${Date.now()}`,
        });
        const target = await db.tag.cmd.create({
            name: `merge-tgt-${Date.now()}`,
        });

        // Build one row in each of the 5 join tables, all attached to source.
        const project = await db.project.cmd.create({
            name: `merge-prj-${Date.now()}`,
        });
        const memory = await db.memory.cmd.create({
            content: 'merge target memory',
            domain: 'backend',
            category: 'fact',
        });
        const artifact = await db.artifact.cmd.create({
            title: `merge-art-${Date.now()}`,
        });
        const milestone = await db.milestone.cmd.create({
            title: `merge-m-${Date.now()}`,
        });
        const task = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'merge task',
        });

        await db.tag.cmd.attachProject({
            tagId: source.tag_id,
            projectId: project.project_id,
        });
        await db.tag.cmd.attachMemory({
            tagId: source.tag_id,
            memoryId: memory.memory_id,
        });
        await db.tag.cmd.attachArtifact({
            tagId: source.tag_id,
            artifactId: artifact.artifact_id,
        });
        await db.tag.cmd.attachMilestone({
            tagId: source.tag_id,
            milestoneId: milestone.milestone_id,
        });
        await db.tag.cmd.attachTask({
            tagId: source.tag_id,
            milestoneId: task.milestone_id,
            taskNo: task.task_no,
        });

        await db.tag.cmd.merge({
            sourceTagId: source.tag_id,
            targetTagId: target.tag_id,
            agentId: agent.agent_id,
            reason: 'collapse',
        });

        // Source is gone.
        const sourceAfter = await db.tag.qry.findById(source.tag_id);
        expect(sourceAfter).toBeUndefined();

        // Each of the 5 join tables now points at target, not source.
        const projectTag = await ctx.kysely
            .selectFrom('Project_Tag')
            .selectAll()
            .where('project_id', '=', project.project_id)
            .execute();
        expect(projectTag.length).toBe(1);
        expect(projectTag[0]?.tag_id).toBe(target.tag_id);

        const memoryTag = await ctx.kysely
            .selectFrom('Memory_Tag')
            .selectAll()
            .where('memory_id', '=', memory.memory_id)
            .execute();
        expect(memoryTag.length).toBe(1);
        expect(memoryTag[0]?.tag_id).toBe(target.tag_id);

        const artifactTag = await ctx.kysely
            .selectFrom('Artifact_Tag')
            .selectAll()
            .where('artifact_id', '=', artifact.artifact_id)
            .execute();
        expect(artifactTag.length).toBe(1);
        expect(artifactTag[0]?.tag_id).toBe(target.tag_id);

        const milestoneTag = await ctx.kysely
            .selectFrom('Milestone_Tag')
            .selectAll()
            .where('milestone_id', '=', milestone.milestone_id)
            .execute();
        expect(milestoneTag.length).toBe(1);
        expect(milestoneTag[0]?.tag_id).toBe(target.tag_id);

        const taskTag = await ctx.kysely
            .selectFrom('Task_Tag')
            .selectAll()
            .where('milestone_id', '=', task.milestone_id)
            .where('task_no', '=', task.task_no)
            .execute();
        expect(taskTag.length).toBe(1);
        expect(taskTag[0]?.tag_id).toBe(target.tag_id);

    });

    it('db.tag.cmd.merge skips rows already on target (no PK conflict)', async () => {

        const agent = await db.agent.cmd.create({
            name: `merge-skip-${Date.now()}`,
        });

        const source = await db.tag.cmd.create({
            name: `skip-src-${Date.now()}`,
        });
        const target = await db.tag.cmd.create({
            name: `skip-tgt-${Date.now()}`,
        });
        const memory = await db.memory.cmd.create({
            content: 'shared memory',
            domain: 'backend',
            category: 'fact',
        });

        await db.tag.cmd.attachMemory({
            tagId: source.tag_id,
            memoryId: memory.memory_id,
        });
        await db.tag.cmd.attachMemory({
            tagId: target.tag_id,
            memoryId: memory.memory_id,
        });

        await db.tag.cmd.merge({
            sourceTagId: source.tag_id,
            targetTagId: target.tag_id,
            agentId: agent.agent_id,
        });

        const memTag = await ctx.kysely
            .selectFrom('Memory_Tag')
            .selectAll()
            .where('memory_id', '=', memory.memory_id)
            .execute();
        expect(memTag.length).toBe(1);
        expect(memTag[0]?.tag_id).toBe(target.tag_id);

    });

});

import { beforeAll, beforeEach, describe, it, expect } from 'bun:test';

import { bootstrap, truncateAll } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];
let db:  Awaited<ReturnType<typeof bootstrap>>['db'];

beforeAll(async () => {

    ({ ctx, db } = await bootstrap());

});

beforeEach(async () => {

    await truncateAll(ctx);

});

describe('db.memory.cmd.consolidate', () => {

    it('moves tags + projects from duplicate to canonical and supersedes the duplicate', async () => {

        const projectId = await db.project.cmd.create({
            name: 'consolidate-host', agentId: 0,
        });
        const tagId = await db.tag.cmd.create({
            name: 'shared-tag', description: '', reason: 'seed',
            provenanceId: 0, agentId: 0,
        });

        const canonicalId = await db.memory.cmd.create({
            content: 'canonical wording', domain: 'backend', category: 'fact',
            reason: 'keeper', provenanceId: 0, agentId: 0, wasObserved: true,
        });
        const duplicateId = await db.memory.cmd.create({
            content: 'duplicate wording', domain: 'backend', category: 'fact',
            reason: 'merge candidate', provenanceId: 0, agentId: 0, wasObserved: true,
        });

        // Attach tag + project to the duplicate so consolidate has work to do.
        await db.tag.cmd.attachMemory({ tagId, memoryId: duplicateId });
        await db.memory.cmd.attachProject({ memoryId: duplicateId, projectId });

        await db.memory.cmd.consolidate({
            canonicalMemoryId: canonicalId,
            duplicateMemoryId: duplicateId,
            agentId:           0,
            reason:            'same fact',
        });

        // Duplicate is now superseded.
        const duplicate = await db.memory.qry.findById(duplicateId);
        if (!duplicate) throw new Error('duplicate not found');
        expect(duplicate.relevance_status).toBe('superseded');

        // The shared tag is now on the canonical memory.
        const canonicalTags = await db.tag.qry.listForMemory(canonicalId);
        expect(canonicalTags.some((row) => row.tag_id === tagId)).toBe(true);

        // And the duplicate's tag attachment is gone.
        const duplicateTags = await db.tag.qry.listForMemory(duplicateId);
        expect(duplicateTags.some((row) => row.tag_id === tagId)).toBe(false);

        // A 'supersedes' edge now exists from canonical to duplicate.
        const links = await db.memory.qry.related(canonicalId);
        expect(links.length).toBeGreaterThan(0);

    });

});

describe('db.tag.cmd.merge', () => {

    it('re-points all 5 *_Tag tables and hard-deletes the source tag', async () => {

        const sourceId = await db.tag.cmd.create({
            name: 'source', description: '', reason: 'seed',
            provenanceId: 0, agentId: 0,
        });
        const targetId = await db.tag.cmd.create({
            name: 'target', description: '', reason: 'seed',
            provenanceId: 0, agentId: 0,
        });

        // Project_Tag.
        const projectId = await db.project.cmd.create({
            name: 'p-merge', filepath: '/p', gitRepo: '', mainBranch: 'main',
            gitUrl: '', agentId: 0,
        });
        await db.tag.cmd.attachProject({ tagId: sourceId, projectId });

        // Memory_Tag.
        const memoryId = await db.memory.cmd.create({
            content: 'memory carrier', domain: 'backend', category: 'fact',
            reason: 'seed', provenanceId: 0, agentId: 0, wasObserved: true,
        });
        await db.tag.cmd.attachMemory({ tagId: sourceId, memoryId });

        // Artifact_Tag.
        const artifactId = await db.artifact.cmd.create({
            title: 'design.md', description: '', filepath: '', reason: 'seed',
            provenanceId: 0, agentId: 0,
        });
        await db.tag.cmd.attachArtifact({ tagId: sourceId, artifactId });

        // Milestone_Tag + Task_Tag.
        const milestoneId = await db.milestone.cmd.create({
            title: 'merge milestone', provenanceId: 0, agentId: 0,
        });
        await db.tag.cmd.attachMilestone({ tagId: sourceId, milestoneId });

        const { taskNo } = await db.task.cmd.create({
            milestoneId, title: 'merge task', agentId: 0,
        });
        await db.tag.cmd.attachTask({ tagId: sourceId, milestoneId, taskNo });

        await db.tag.cmd.merge({
            sourceTagId: sourceId,
            targetTagId: targetId,
            agentId:     0,
            reason:      'consolidate duplicates',
        });

        // Source is hard-deleted.
        const source = await db.tag.qry.findById(sourceId);
        expect(source).toBeUndefined();

        // Target now carries every attachment.
        const projectAttach = await db.tag.qry.listForProject(projectId);
        expect(projectAttach.some((r) => r.tag_id === targetId)).toBe(true);

        const memoryAttach = await db.tag.qry.listForMemory(memoryId);
        expect(memoryAttach.some((r) => r.tag_id === targetId)).toBe(true);

        const artifactAttach = await db.tag.qry.listForArtifact(artifactId);
        expect(artifactAttach.some((r) => r.tag_id === targetId)).toBe(true);

        const milestoneAttach = await db.tag.qry.listForMilestone(milestoneId);
        expect(milestoneAttach.some((r) => r.tag_id === targetId)).toBe(true);

        const taskAttach = await db.tag.qry.listForTask(milestoneId, taskNo);
        expect(taskAttach.some((r) => r.tag_id === targetId)).toBe(true);

    });

});

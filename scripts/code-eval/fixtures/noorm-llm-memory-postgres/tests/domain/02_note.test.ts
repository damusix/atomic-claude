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

async function seedProject(): Promise<number> {

    return db.project.cmd.create({ name: 'note-test-project', agentId: 0 });

}

async function seedMilestone(): Promise<number> {

    return db.milestone.cmd.create({
        title: 'Note milestone', provenanceId: 0, agentId: 0,
    });

}

async function seedTask(milestoneId: number): Promise<{ milestoneId: number; taskNo: number }> {

    return db.task.cmd.create({
        milestoneId, title: 'Note task', agentId: 0,
    });

}

describe('db.note.cmd.createProject', () => {

    it('creates a project-typed note and returns its id', async () => {

        const projectId = await seedProject();

        const noteId = await db.note.cmd.createProject({
            content: 'Kickoff doc posted.', reason: 'planning',
            provenanceId: projectId, agentId: 0, projectId,
        });

        expect(noteId).toBeGreaterThan(0);

        const row = await db.note.qry.findById(noteId);
        if (!row) throw new Error('findById returned undefined');

        expect(row.note_type).toBe('project');
        expect(row.project_id).toBe(projectId);

    });

});

describe('db.note.cmd.createMilestone', () => {

    it('creates a milestone-typed note', async () => {

        const milestoneId = await seedMilestone();

        const noteId = await db.note.cmd.createMilestone({
            content: 'M1 scope frozen.', reason: 'review',
            provenanceId: 0, agentId: 0, milestoneId,
        });

        const row = await db.note.qry.findById(noteId);
        if (!row) throw new Error('findById returned undefined');

        expect(row.note_type).toBe('milestone');
        expect(row.milestone_id).toBe(milestoneId);

    });

});

describe('db.note.cmd.createTask', () => {

    it('creates a task-typed note', async () => {

        const milestoneId = await seedMilestone();
        const { taskNo } = await seedTask(milestoneId);

        const noteId = await db.note.cmd.createTask({
            content: 'Spike unblocked.', reason: 'progress',
            provenanceId: 0, agentId: 0, milestoneId, taskNo,
        });

        const row = await db.note.qry.findById(noteId);
        if (!row) throw new Error('findById returned undefined');

        expect(row.note_type).toBe('task');
        expect(row.task_no).toBe(taskNo);
        expect(row.milestone_id).toBe(milestoneId);

    });

});

describe('db.note.cmd.update', () => {

    it('rewrites content and reason', async () => {

        const milestoneId = await seedMilestone();
        const noteId = await db.note.cmd.createMilestone({
            content: 'original', reason: 'seed',
            provenanceId: 0, agentId: 0, milestoneId,
        });

        await db.note.cmd.update({
            noteId, content: 'edited', reason: 'fix typo',
        });

        const row = await db.note.qry.findById(noteId);
        if (!row) throw new Error('findById returned undefined');

        expect(row.content).toBe('edited');
        expect(row.reason).toBe('fix typo');

    });

});

describe('db.note.cmd.setRelevance', () => {

    it('moves a note from active to needs-review', async () => {

        const milestoneId = await seedMilestone();
        const noteId = await db.note.cmd.createMilestone({
            content: 'subject', reason: 'seed',
            provenanceId: 0, agentId: 0, milestoneId,
        });

        await db.note.cmd.setRelevance({
            noteId, newRelevanceStatus: 'needs-review', agentId: 0, reason: 'audit',
        });

        const row = await db.note.qry.findById(noteId);
        if (!row) throw new Error('findById returned undefined');

        expect(row.relevance_status).toBe('needs-review');

    });

});

describe('db.note.cmd.softDelete + restore', () => {

    it('round-trips relevance through deleted then back to active', async () => {

        const milestoneId = await seedMilestone();
        const noteId = await db.note.cmd.createMilestone({
            content: 'reversible', reason: 'seed',
            provenanceId: 0, agentId: 0, milestoneId,
        });

        await db.note.cmd.softDelete({ noteId, agentId: 0, reason: 'remove' });

        const deleted = await db.note.qry.findById(noteId);
        if (!deleted) throw new Error('findById returned undefined');
        expect(deleted.relevance_status).toBe('deleted');

        await db.note.cmd.restore({ noteId, agentId: 0, reason: 'put back' });

        const restored = await db.note.qry.findById(noteId);
        if (!restored) throw new Error('findById returned undefined');
        expect(restored.relevance_status).toBe('active');

    });

});

describe('Zod failures on note inputs', () => {

    it('rejects empty content on createProject', async () => {

        await expect(
            db.note.cmd.createProject({
                content: '', reason: '',
                provenanceId: 0, agentId: 0, projectId: 1,
            }),
        ).rejects.toThrow();

    });

    it('rejects an invalid noteType filter via Zod on listActive', async () => {

        await expect(
            db.note.qry.listActive({ noteType: 'not-a-valid-type', limit: 10 }),
        ).rejects.toThrow();

    });

});

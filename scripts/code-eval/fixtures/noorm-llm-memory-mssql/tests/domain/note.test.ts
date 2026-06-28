/**
 * Layer 2 — NoteCommands / NoteQueries facade.
 *
 * The three create variants exercise the exclusive-subtype discriminator;
 * setRelevance / delete / restore drive the relevance state machine.
 * Verifies camelCase mapping (e.g. milestoneId / taskNo) and Zod guards.
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

describe('note: project subtype', () => {

    it('db.note.cmd.createForProject + qry.findById round-trip', async () => {

        const project = await db.project.cmd.create({
            name: `note-project-${Date.now()}`,
        });

        const created = await db.note.cmd.createForProject({
            projectId: project.project_id,
            content: 'project-scoped note',
        });

        expect(created.note_id).toBeGreaterThan(0);

        const fetched = await db.note.qry.findById(created.note_id);
        expect(fetched?.content).toBe('project-scoped note');
        expect(fetched?.note_type).toBe('project');

    });

    it('db.note.qry.findInView resolves project_id via the subtype join', async () => {

        const project = await db.project.cmd.create({
            name: `note-view-${Date.now()}`,
        });
        const { note_id } = await db.note.cmd.createForProject({
            projectId: project.project_id,
            content: 'view me',
        });

        const view = await db.note.qry.findInView(note_id);
        expect(view?.note_id).toBe(note_id);
        expect(view?.project_id).toBe(project.project_id);

    });

});

describe('note: milestone subtype', () => {

    it('db.note.cmd.createForMilestone attaches via the milestone subtype', async () => {

        const milestone = await db.milestone.cmd.create({
            title: `m-note-${Date.now()}`,
        });

        const { note_id } = await db.note.cmd.createForMilestone({
            milestoneId: milestone.milestone_id,
            content: 'milestone note',
        });

        const view = await db.note.qry.findInView(note_id);
        expect(view?.milestone_id).toBe(milestone.milestone_id);
        expect(view?.note_type).toBe('milestone');

    });

});

describe('note: task subtype', () => {

    it('db.note.cmd.createForTask attaches via the task subtype', async () => {

        const milestone = await db.milestone.cmd.create({
            title: `task-note-${Date.now()}`,
        });
        const task = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'task for note',
        });

        const { note_id } = await db.note.cmd.createForTask({
            milestoneId: task.milestone_id,
            taskNo: task.task_no,
            content: 'task note',
        });

        const view = await db.note.qry.findInView(note_id);
        expect(view?.note_type).toBe('task');
        expect(view?.milestone_id).toBe(task.milestone_id);
        expect(view?.task_no).toBe(task.task_no);

    });

});

describe('note: list + state machine', () => {

    it('db.note.qry.list returns rows ordered ascending', async () => {

        const project = await db.project.cmd.create({
            name: `list-${Date.now()}`,
        });

        const a = await db.note.cmd.createForProject({
            projectId: project.project_id,
            content: 'first',
        });
        const b = await db.note.cmd.createForProject({
            projectId: project.project_id,
            content: 'second',
        });

        const all = await db.note.qry.list();
        const ids = all.map((n) => n.note_id);
        expect(ids).toContain(a.note_id);
        expect(ids).toContain(b.note_id);
        const aIdx = ids.indexOf(a.note_id);
        const bIdx = ids.indexOf(b.note_id);
        expect(aIdx).toBeLessThan(bIdx);

    });

    it('db.note.qry.listInView returns vw_Note rows', async () => {

        const milestone = await db.milestone.cmd.create({
            title: `view-list-${Date.now()}`,
        });

        await db.note.cmd.createForMilestone({
            milestoneId: milestone.milestone_id,
            content: 'view list me',
        });

        const view = await db.note.qry.listInView();
        expect(view.length).toBeGreaterThanOrEqual(1);

    });

    it('db.note.cmd.setRelevance / delete / restore drives the state machine', async () => {

        const project = await db.project.cmd.create({
            name: `note-sm-${Date.now()}`,
        });
        const agent = await db.agent.cmd.create({
            name: `note-sm-agent-${Date.now()}`,
        });

        const { note_id } = await db.note.cmd.createForProject({
            projectId: project.project_id,
            content: 'sm target',
        });

        await db.note.cmd.setRelevance({
            noteId: note_id,
            newRelevanceStatus: 'needs-review',
            agentId: agent.agent_id,
        });

        const reviewed = await db.note.qry.findById(note_id);
        expect(reviewed?.relevance_status).toBe('needs-review');

        await db.note.cmd.delete({ noteId: note_id, agentId: agent.agent_id });
        const deleted = await db.note.qry.findById(note_id);
        expect(deleted?.relevance_status).toBe('deleted');

        await db.note.cmd.restore({ noteId: note_id, agentId: agent.agent_id });
        const restored = await db.note.qry.findById(note_id);
        expect(restored?.relevance_status).toBe('active');

    });

    it('db.note.cmd.update modifies content', async () => {

        const project = await db.project.cmd.create({
            name: `note-upd-${Date.now()}`,
        });
        const { note_id } = await db.note.cmd.createForProject({
            projectId: project.project_id,
            content: 'before',
        });

        await db.note.cmd.update({ noteId: note_id, content: 'after' });

        const after = await db.note.qry.findById(note_id);
        expect(after?.content).toBe('after');

    });

});

describe('note: zod boundary', () => {

    it('db.note.cmd.createForProject rejects negative projectId', async () => {

        await expect(db.note.cmd.createForProject({
            projectId: -1,
            content: 'bad',
        })).rejects.toThrow();

    });

    it('db.note.cmd.createForTask rejects zero taskNo (.positive())', async () => {

        await expect(db.note.cmd.createForTask({
            milestoneId: 1,
            taskNo: 0,
            content: 'bad',
        })).rejects.toThrow();

    });

    it('db.note.cmd.setRelevance rejects empty newRelevanceStatus', async () => {

        await expect(db.note.cmd.setRelevance({
            noteId: 1,
            newRelevanceStatus: '',
            agentId: 0,
        })).rejects.toThrow();

    });

});

import { beforeAll, beforeEach, describe, it, expect } from 'bun:test';

import { bootstrap, truncateAll } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});

beforeEach(async () => {

    await truncateAll(ctx);

});

async function makeMemory(content = 'm', evidenced = false): Promise<number> {

    const result = await ctx.proc('sp_Memory_Create', {
        p_content: content, p_domain: 'backend', p_category: 'fact',
        p_reason: '', p_provenance_id: 0, p_agent_id: 0,
        p_was_inferred: false, p_was_observed: true,
        p_was_evidenced: evidenced, p_was_user_provided: false,
    });
    const [created] = result;
    if (!created) throw new Error('memory create failed');
    return created.memory_id;

}

async function makeMilestone(title = 'M'): Promise<number> {

    const result = await ctx.proc('sp_Milestone_Create', {
        p_title: title, p_content: '', p_reason: '',
        p_provenance_id: 0, p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('milestone create failed');
    return created.milestone_id;

}

async function makeTask(milestoneId: number, title = 'T'): Promise<{ milestone_id: number; task_no: number }> {

    const result = await ctx.proc('sp_Task_Create', {
        p_milestone_id: milestoneId, p_title: title, p_content: '',
        p_reason: '', p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('task create failed');
    return created;

}

async function makeArtifact(title = 'doc.md'): Promise<number> {

    const result = await ctx.proc('sp_Artifact_Create', {
        p_title: title, p_description: '', p_filepath: title,
        p_reason: '', p_provenance_id: 0, p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('artifact create failed');
    return created.artifact_id;

}

async function makeProject(name = 'p'): Promise<number> {

    const result = await ctx.proc('sp_Project_Create', {
        p_name: name, p_filepath: '/p', p_git_repo: '', p_main_branch: 'main',
        p_git_url: '', p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('project create failed');
    return created.project_id;

}

async function makeAgent(name: string, description = ''): Promise<number> {

    const result = await ctx.proc('sp_Agent_Create', {
        p_name: name, p_description: description,
    });
    const [created] = result;
    if (!created) throw new Error('agent create failed');
    return created.agent_id;

}

async function makeTag(name: string): Promise<number> {

    const result = await ctx.proc('sp_Tag_Create', {
        p_name: name, p_description: '', p_reason: '',
        p_provenance_id: 0, p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('tag create failed');
    return created.tag_id;

}

describe('vw_Memory', () => {

    it('exposes confidence equal to the count of true was_* booleans', async () => {

        const id = await makeMemory('with confidence', true);
        // was_observed=true + was_evidenced=true = 2

        const row = await ctx.kysely.selectFrom('vw_Memory')
            .selectAll()
            .where('memory_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(row.confidence).toBe(2);

    });

});

describe('vw_Tag', () => {

    it('returns one row per attachment when a tag is attached to multiple memories', async () => {

        const tagId = await makeTag('multi');

        const m1 = await makeMemory('m1');
        const m2 = await makeMemory('m2');

        await ctx.proc('sp_Tag_Attach_Memory', { p_tag_id: tagId, p_memory_id: m1 });
        await ctx.proc('sp_Tag_Attach_Memory', { p_tag_id: tagId, p_memory_id: m2 });

        const rows = await ctx.kysely.selectFrom('vw_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .execute();

        expect(rows.length).toBe(2);
        for (const r of rows) {

            expect(r.relation_type).toBe('memory');

        }

        const memoryIds = rows.map((r) => r.memory_id).sort();
        expect(memoryIds).toEqual([m1, m2].sort());

    });

    it('exposes one row per non-Memory branch with the correct discriminator', async () => {

        const tagId = await makeTag('span');

        const projectId   = await makeProject('p-span');
        const artifactId  = await makeArtifact('a-span.md');
        const milestoneId = await makeMilestone('m-span');
        const task        = await makeTask(milestoneId, 't-span');

        await ctx.proc('sp_Tag_Attach_Project',   { p_tag_id: tagId, p_project_id:   projectId });
        await ctx.proc('sp_Tag_Attach_Artifact',  { p_tag_id: tagId, p_artifact_id:  artifactId });
        await ctx.proc('sp_Tag_Attach_Milestone', { p_tag_id: tagId, p_milestone_id: milestoneId });
        await ctx.proc('sp_Tag_Attach_Task',      {
            p_tag_id:       tagId,
            p_milestone_id: task.milestone_id,
            p_task_no:      task.task_no,
        });

        const rows = await ctx.kysely.selectFrom('vw_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .execute();

        expect(rows.length).toBe(4);

        const byType = new Map(rows.map((r) => [r.relation_type, r]));

        const projectRow = byType.get('project');
        if (!projectRow) throw new Error('expected project branch');
        expect(projectRow.project_id).toBe(projectId);
        expect(projectRow.memory_id).toBe(0);
        expect(projectRow.artifact_id).toBe(0);
        expect(projectRow.milestone_id).toBe(0);
        expect(projectRow.task_no).toBe(0);

        const artifactRow = byType.get('artifact');
        if (!artifactRow) throw new Error('expected artifact branch');
        expect(artifactRow.artifact_id).toBe(artifactId);
        expect(artifactRow.project_id).toBe(0);
        expect(artifactRow.milestone_id).toBe(0);
        expect(artifactRow.task_no).toBe(0);

        const milestoneRow = byType.get('milestone');
        if (!milestoneRow) throw new Error('expected milestone branch');
        expect(milestoneRow.milestone_id).toBe(milestoneId);
        expect(milestoneRow.task_no).toBe(0);
        expect(milestoneRow.project_id).toBe(0);
        expect(milestoneRow.artifact_id).toBe(0);

        const taskRow = byType.get('task');
        if (!taskRow) throw new Error('expected task branch');
        expect(taskRow.milestone_id).toBe(task.milestone_id);
        expect(taskRow.task_no).toBe(task.task_no);
        expect(taskRow.project_id).toBe(0);
        expect(taskRow.artifact_id).toBe(0);

    });

});

describe('vw_Related_Memory', () => {

    it('exposes both forward and backward directions for a stored edge', async () => {

        const a = await makeMemory('A');
        const b = await makeMemory('B');

        await ctx.proc('sp_Memory_Relate', {
            p_memory_id: a, p_related_memory_id: b,
            p_relation_verb: 'supersedes', p_reason: 'B replaces A',
        });

        const fromA = await ctx.kysely.selectFrom('vw_Related_Memory')
            .selectAll()
            .where('memory_id', '=', a)
            .where('related_memory_id', '=', b)
            .executeTakeFirstOrThrow();

        expect(fromA.verb).toBe('supersedes');

        const fromB = await ctx.kysely.selectFrom('vw_Related_Memory')
            .selectAll()
            .where('memory_id', '=', b)
            .where('related_memory_id', '=', a)
            .executeTakeFirstOrThrow();

        expect(fromB.verb).toBe('superseded-by');

    });

});

describe('vw_Milestone_Stats', () => {

    it('aggregates rolled-up task / artifact / note / tag / dep / project counts', async () => {

        const id = await makeMilestone('stats-ms');

        const t1 = await makeTask(id, 'open');           // not-started
        const t2 = await makeTask(id, 'done');           // -> done
        const t3 = await makeTask(id, 'abandoned');      // -> abandoned
        const t4 = await makeTask(id, 'still-open');     // -> in-progress
        const blocked = await makeTask(id, 'blocked');   // depends on t1 (not-done)

        // t2: not-started -> in-progress -> done
        await ctx.proc('sp_Task_SetTracking', {
            p_milestone_id: t2.milestone_id, p_task_no: t2.task_no,
            p_new_tracking_status: 'in-progress', p_agent_id: 0, p_reason: '',
        });
        await ctx.proc('sp_Task_SetTracking', {
            p_milestone_id: t2.milestone_id, p_task_no: t2.task_no,
            p_new_tracking_status: 'done', p_agent_id: 0, p_reason: '',
        });

        // t3 -> abandoned
        await ctx.proc('sp_Task_SetTracking', {
            p_milestone_id: t3.milestone_id, p_task_no: t3.task_no,
            p_new_tracking_status: 'abandoned', p_agent_id: 0, p_reason: '',
        });

        // t4 -> in-progress (still open)
        await ctx.proc('sp_Task_SetTracking', {
            p_milestone_id: t4.milestone_id, p_task_no: t4.task_no,
            p_new_tracking_status: 'in-progress', p_agent_id: 0, p_reason: '',
        });

        // blocked depends on t1 (not-started) via 'blocks'
        await ctx.proc('sp_Task_Depend', {
            p_milestone_id:     blocked.milestone_id, p_task_no:     blocked.task_no,
            p_dep_milestone_id: t1.milestone_id,      p_dep_task_no: t1.task_no,
            p_dependency_verb:  'blocks',             p_reason:      'blocked-by t1',
        });

        // Artifacts: 1 attached to milestone, 1 attached to task t1
        const a1 = await makeArtifact('a1.md');
        const a2 = await makeArtifact('a2.md');
        await ctx.proc('sp_Artifact_Attach_Milestone', { p_artifact_id: a1, p_milestone_id: id });
        await ctx.proc('sp_Artifact_Attach_Task', {
            p_artifact_id: a2, p_milestone_id: t1.milestone_id, p_task_no: t1.task_no,
        });

        // Notes: 1 milestone-note, 1 task-note (under t1)
        await ctx.proc('sp_Note_Create_Milestone', {
            p_content: 'mn', p_reason: '', p_provenance_id: 0, p_agent_id: 0,
            p_milestone_id: id,
        });
        await ctx.proc('sp_Note_Create_Task', {
            p_content: 'tn', p_reason: '', p_provenance_id: 0, p_agent_id: 0,
            p_milestone_id: t1.milestone_id, p_task_no: t1.task_no,
        });

        // Tags: 1 attached to milestone, 1 attached to a task
        const tag1 = await makeTag('tg-ms');
        const tag2 = await makeTag('tg-task');
        await ctx.proc('sp_Tag_Attach_Milestone', { p_tag_id: tag1, p_milestone_id: id });
        await ctx.proc('sp_Tag_Attach_Task', {
            p_tag_id: tag2, p_milestone_id: t1.milestone_id, p_task_no: t1.task_no,
        });

        // Project link
        const projectId = await makeProject('p-stats');
        await ctx.proc('sp_Milestone_Attach_Project', { p_milestone_id: id, p_project_id: projectId });

        const stats = await ctx.kysely.selectFrom('vw_Milestone_Stats')
            .selectAll()
            .where('milestone_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(stats.total_tasks).toBe(5);
        expect(stats.done_tasks).toBe(1);
        expect(stats.abandoned_tasks).toBe(1);
        // open = 5 - done - abandoned = 3 (t1 not-started, t4 in-progress, blocked not-started)
        expect(stats.open_tasks).toBe(3);
        // 'blocked' depends on t1 which is not 'done' -> 1 blocked task
        expect(stats.blocked_tasks).toBe(1);

        expect(stats.total_artifacts).toBe(2);
        expect(stats.total_notes).toBe(2);
        expect(stats.total_tags).toBe(2);
        // 1 dep edge: blocked -> t1
        expect(stats.total_dependencies).toBe(1);
        expect(stats.project_count).toBe(1);

    });

});

describe('vw_Recent_Activity', () => {

    it('orders most-recent first when sorted by occurred_at DESC', async () => {

        const m1 = await makeMemory('first');
        // tiny pause so the second created_at is strictly later
        await new Promise((r) => setTimeout(r, 25));
        const m2 = await makeMemory('second');

        const rows = await ctx.kysely.selectFrom('vw_Recent_Activity')
            .selectAll()
            .where('entity_type', '=', 'memory')
            .where('action_type', '=', 'created')
            .where('entity_id', 'in', [m1, m2])
            .orderBy('occurred_at', 'desc')
            .execute();

        expect(rows.length).toBe(2);
        const [first, second] = rows;
        if (!first || !second) throw new Error('expected 2 rows');
        expect(first.entity_id).toBe(m2);
        expect(second.entity_id).toBe(m1);
        expect(first.occurred_at.getTime()).toBeGreaterThanOrEqual(second.occurred_at.getTime());

    });

    it('emits an updated row after sp_Memory_Update with the new content excerpt', async () => {

        const memoryAgent = await makeAgent('updater');

        const result = await ctx.proc('sp_Memory_Create', {
            p_content: 'before-update', p_domain: 'backend', p_category: 'fact',
            p_reason: '', p_provenance_id: 0, p_agent_id: memoryAgent,
            p_was_inferred: false, p_was_observed: true,
            p_was_evidenced: false, p_was_user_provided: false,
        });
        const [created] = result;
        if (!created) throw new Error('memory create failed');

        // Force updated_at strictly after created_at so the 'updated' branch matches.
        await new Promise((r) => setTimeout(r, 25));

        await ctx.proc('sp_Memory_Update', {
            p_memory_id:         created.memory_id,
            p_content:           'after-update-text',
            p_domain:            'backend',
            p_category:          'fact',
            p_reason:            'edited',
            p_was_inferred:      false,
            p_was_observed:      true,
            p_was_evidenced:     true,
            p_was_user_provided: false,
        });

        const updatedRow = await ctx.kysely.selectFrom('vw_Recent_Activity')
            .selectAll()
            .where('entity_type', '=', 'memory')
            .where('entity_id', '=', created.memory_id)
            .where('action_type', '=', 'updated')
            .executeTakeFirstOrThrow();

        expect(updatedRow.title_or_excerpt).toBe('after-update-text');
        expect(updatedRow.agent_id).toBe(memoryAgent);

    });

    it('emits a transitioned row when a relevance status changes', async () => {

        const transitionAgent = await makeAgent('transitioner');

        const result = await ctx.proc('sp_Memory_Create', {
            p_content: 'transition-me', p_domain: 'backend', p_category: 'fact',
            p_reason: '', p_provenance_id: 0, p_agent_id: 0,
            p_was_inferred: false, p_was_observed: true,
            p_was_evidenced: false, p_was_user_provided: false,
        });
        const [created] = result;
        if (!created) throw new Error('memory create failed');

        await ctx.proc('sp_Memory_SetRelevance', {
            p_memory_id:            created.memory_id,
            p_new_relevance_status: 'needs-review',
            p_agent_id:             transitionAgent,
            p_reason:               'flagged-for-review',
        });

        const transitionedRow = await ctx.kysely.selectFrom('vw_Recent_Activity')
            .selectAll()
            .where('entity_type', '=', 'memory')
            .where('entity_id', '=', created.memory_id)
            .where('action_type', '=', 'transitioned')
            .executeTakeFirstOrThrow();

        // For transitioned rows, title_or_excerpt = StateTransition.reason.
        expect(transitionedRow.title_or_excerpt).toBe('flagged-for-review');
        expect(transitionedRow.agent_id).toBe(transitionAgent);

    });

});

describe('vw_Note', () => {

    it('returns one row per note with the discriminator and zeroed unrelated entity columns', async () => {

        const projectId = await makeProject('p-note');
        const milestoneId = await makeMilestone('mn-host');
        const task = await makeTask(milestoneId, 'tn-host');

        const pn = await ctx.proc('sp_Note_Create_Project', {
            p_content: 'pn', p_reason: '', p_provenance_id: 0, p_agent_id: 0,
            p_project_id: projectId,
        });
        const [pnRow] = pn;
        if (!pnRow) throw new Error('pn create failed');

        const mn = await ctx.proc('sp_Note_Create_Milestone', {
            p_content: 'mn', p_reason: '', p_provenance_id: 0, p_agent_id: 0,
            p_milestone_id: milestoneId,
        });
        const [mnRow] = mn;
        if (!mnRow) throw new Error('mn create failed');

        const tn = await ctx.proc('sp_Note_Create_Task', {
            p_content: 'tn', p_reason: '', p_provenance_id: 0, p_agent_id: 0,
            p_milestone_id: task.milestone_id, p_task_no: task.task_no,
        });
        const [tnRow] = tn;
        if (!tnRow) throw new Error('tn create failed');

        const projectNote = await ctx.kysely.selectFrom('vw_Note')
            .selectAll()
            .where('note_id', '=', pnRow.note_id)
            .executeTakeFirstOrThrow();

        expect(projectNote.note_type).toBe('project');
        expect(projectNote.project_id).toBe(projectId);
        expect(projectNote.milestone_id).toBe(0);
        expect(projectNote.task_no).toBe(0);

        const milestoneNote = await ctx.kysely.selectFrom('vw_Note')
            .selectAll()
            .where('note_id', '=', mnRow.note_id)
            .executeTakeFirstOrThrow();

        expect(milestoneNote.note_type).toBe('milestone');
        expect(milestoneNote.project_id).toBe(0);
        expect(milestoneNote.milestone_id).toBe(milestoneId);
        expect(milestoneNote.task_no).toBe(0);

        const taskNote = await ctx.kysely.selectFrom('vw_Note')
            .selectAll()
            .where('note_id', '=', tnRow.note_id)
            .executeTakeFirstOrThrow();

        expect(taskNote.note_type).toBe('task');
        expect(taskNote.project_id).toBe(0);
        expect(taskNote.milestone_id).toBe(task.milestone_id);
        expect(taskNote.task_no).toBe(task.task_no);

    });

});

describe('vw_Artifact', () => {

    it('returns one row per artifact-attachment with the discriminator and zeroed task_no for milestone branch', async () => {

        const milestoneId = await makeMilestone('art-host');
        const task = await makeTask(milestoneId, 'art-task');

        const a1 = await makeArtifact('attached-to-milestone.md');
        const a2 = await makeArtifact('attached-to-task.md');

        await ctx.proc('sp_Artifact_Attach_Milestone', { p_artifact_id: a1, p_milestone_id: milestoneId });
        await ctx.proc('sp_Artifact_Attach_Task', {
            p_artifact_id: a2, p_milestone_id: task.milestone_id, p_task_no: task.task_no,
        });

        const rows = await ctx.kysely.selectFrom('vw_Artifact')
            .selectAll()
            .where('artifact_id', 'in', [a1, a2])
            .execute();

        expect(rows.length).toBe(2);

        const milestoneRow = rows.find((r) => r.relation_type === 'milestone');
        if (!milestoneRow) throw new Error('missing milestone branch');
        expect(milestoneRow.artifact_id).toBe(a1);
        expect(milestoneRow.milestone_id).toBe(milestoneId);
        expect(milestoneRow.task_no).toBe(0);

        const taskRow = rows.find((r) => r.relation_type === 'task');
        if (!taskRow) throw new Error('missing task branch');
        expect(taskRow.artifact_id).toBe(a2);
        expect(taskRow.milestone_id).toBe(task.milestone_id);
        expect(taskRow.task_no).toBe(task.task_no);

    });

});

describe('vw_Active_Memory', () => {

    it('shows only memories with relevance_status = active', async () => {

        const active = await makeMemory('active-one');
        const toDelete = await makeMemory('to-delete');

        await ctx.proc('sp_Memory_Delete', {
            p_memory_id: toDelete, p_agent_id: 0, p_reason: 'cleanup',
        });

        const rows = await ctx.kysely.selectFrom('vw_Active_Memory')
            .selectAll()
            .where('memory_id', 'in', [active, toDelete])
            .execute();

        expect(rows.length).toBe(1);
        const [row] = rows;
        if (!row) throw new Error('expected 1 row');
        expect(row.memory_id).toBe(active);
        expect(row.relevance_status).toBe('active');

    });

});

describe('vw_Active_Note', () => {

    it('shows only notes with relevance_status = active', async () => {

        const aResult = await ctx.proc('sp_Note_Create_Project', {
            p_content: 'a', p_reason: '', p_provenance_id: 0, p_agent_id: 0, p_project_id: 0,
        });
        const [aRow] = aResult;
        if (!aRow) throw new Error('note a create failed');

        const dResult = await ctx.proc('sp_Note_Create_Project', {
            p_content: 'd', p_reason: '', p_provenance_id: 0, p_agent_id: 0, p_project_id: 0,
        });
        const [dRow] = dResult;
        if (!dRow) throw new Error('note d create failed');

        await ctx.proc('sp_Note_Delete', {
            p_note_id: dRow.note_id, p_agent_id: 0, p_reason: 'cleanup',
        });

        const rows = await ctx.kysely.selectFrom('vw_Active_Note')
            .selectAll()
            .where('note_id', 'in', [aRow.note_id, dRow.note_id])
            .execute();

        expect(rows.length).toBe(1);
        const [row] = rows;
        if (!row) throw new Error('expected 1 row');
        expect(row.note_id).toBe(aRow.note_id);
        expect(row.relevance_status).toBe('active');

    });

});

describe('vw_Active_Artifact', () => {

    it('shows only artifacts with relevance_status = active', async () => {

        const active = await makeArtifact('keep.md');
        const toDelete = await makeArtifact('drop.md');

        await ctx.proc('sp_Artifact_Delete', {
            p_artifact_id: toDelete, p_agent_id: 0, p_reason: 'cleanup',
        });

        const rows = await ctx.kysely.selectFrom('vw_Active_Artifact')
            .selectAll()
            .where('artifact_id', 'in', [active, toDelete])
            .execute();

        expect(rows.length).toBe(1);
        const [row] = rows;
        if (!row) throw new Error('expected 1 row');
        expect(row.artifact_id).toBe(active);
        expect(row.relevance_status).toBe('active');

    });

});

describe('vw_Active_Milestone', () => {

    it('shows only milestones with relevance_status = active', async () => {

        const active = await makeMilestone('keep');
        const toDelete = await makeMilestone('drop');

        await ctx.proc('sp_Milestone_Delete', {
            p_milestone_id: toDelete, p_agent_id: 0, p_reason: 'cleanup',
        });

        const rows = await ctx.kysely.selectFrom('vw_Active_Milestone')
            .selectAll()
            .where('milestone_id', 'in', [active, toDelete])
            .execute();

        expect(rows.length).toBe(1);
        const [row] = rows;
        if (!row) throw new Error('expected 1 row');
        expect(row.milestone_id).toBe(active);
        expect(row.relevance_status).toBe('active');

    });

});

describe('vw_Deleted_Memory', () => {

    it('shows only memories with relevance_status = deleted', async () => {

        const active = await makeMemory('active-one');
        const toDelete = await makeMemory('to-delete');

        await ctx.proc('sp_Memory_Delete', {
            p_memory_id: toDelete, p_agent_id: 0, p_reason: 'cleanup',
        });

        const rows = await ctx.kysely.selectFrom('vw_Deleted_Memory')
            .selectAll()
            .where('memory_id', 'in', [active, toDelete])
            .execute();

        expect(rows.length).toBe(1);
        const [row] = rows;
        if (!row) throw new Error('expected 1 row');
        expect(row.memory_id).toBe(toDelete);
        expect(row.relevance_status).toBe('deleted');

    });

});

describe('vw_Deleted_Note', () => {

    it('shows only notes with relevance_status = deleted', async () => {

        const aResult = await ctx.proc('sp_Note_Create_Project', {
            p_content: 'a', p_reason: '', p_provenance_id: 0, p_agent_id: 0, p_project_id: 0,
        });
        const [aRow] = aResult;
        if (!aRow) throw new Error('note a create failed');

        const dResult = await ctx.proc('sp_Note_Create_Project', {
            p_content: 'd', p_reason: '', p_provenance_id: 0, p_agent_id: 0, p_project_id: 0,
        });
        const [dRow] = dResult;
        if (!dRow) throw new Error('note d create failed');

        await ctx.proc('sp_Note_Delete', {
            p_note_id: dRow.note_id, p_agent_id: 0, p_reason: 'cleanup',
        });

        const rows = await ctx.kysely.selectFrom('vw_Deleted_Note')
            .selectAll()
            .where('note_id', 'in', [aRow.note_id, dRow.note_id])
            .execute();

        expect(rows.length).toBe(1);
        const [row] = rows;
        if (!row) throw new Error('expected 1 row');
        expect(row.note_id).toBe(dRow.note_id);
        expect(row.relevance_status).toBe('deleted');

    });

});

describe('vw_Deleted_Artifact', () => {

    it('shows only artifacts with relevance_status = deleted', async () => {

        const active = await makeArtifact('keep.md');
        const toDelete = await makeArtifact('drop.md');

        await ctx.proc('sp_Artifact_Delete', {
            p_artifact_id: toDelete, p_agent_id: 0, p_reason: 'cleanup',
        });

        const rows = await ctx.kysely.selectFrom('vw_Deleted_Artifact')
            .selectAll()
            .where('artifact_id', 'in', [active, toDelete])
            .execute();

        expect(rows.length).toBe(1);
        const [row] = rows;
        if (!row) throw new Error('expected 1 row');
        expect(row.artifact_id).toBe(toDelete);
        expect(row.relevance_status).toBe('deleted');

    });

});

describe('vw_Deleted_Milestone', () => {

    it('shows only milestones with relevance_status = deleted', async () => {

        const active = await makeMilestone('keep');
        const toDelete = await makeMilestone('drop');

        await ctx.proc('sp_Milestone_Delete', {
            p_milestone_id: toDelete, p_agent_id: 0, p_reason: 'cleanup',
        });

        const rows = await ctx.kysely.selectFrom('vw_Deleted_Milestone')
            .selectAll()
            .where('milestone_id', 'in', [active, toDelete])
            .execute();

        expect(rows.length).toBe(1);
        const [row] = rows;
        if (!row) throw new Error('expected 1 row');
        expect(row.milestone_id).toBe(toDelete);
        expect(row.relevance_status).toBe('deleted');

    });

});

describe('vw_Task_Backlog', () => {

    it('returns only open tasks under active milestones, with is_blocked reflecting unresolved blocks deps', async () => {

        const milestoneId = await makeMilestone('backlog-active');

        const notStarted = await makeTask(milestoneId, 'not-started');
        const inProgress = await makeTask(milestoneId, 'in-progress');
        const doneTask   = await makeTask(milestoneId, 'done');

        await ctx.proc('sp_Task_SetTracking', {
            p_milestone_id: inProgress.milestone_id, p_task_no: inProgress.task_no,
            p_new_tracking_status: 'in-progress', p_agent_id: 0, p_reason: '',
        });

        await ctx.proc('sp_Task_SetTracking', {
            p_milestone_id: doneTask.milestone_id, p_task_no: doneTask.task_no,
            p_new_tracking_status: 'in-progress', p_agent_id: 0, p_reason: '',
        });
        await ctx.proc('sp_Task_SetTracking', {
            p_milestone_id: doneTask.milestone_id, p_task_no: doneTask.task_no,
            p_new_tracking_status: 'done', p_agent_id: 0, p_reason: '',
        });

        // notStarted blocks inProgress — inProgress should now be is_blocked=true,
        // notStarted should be is_blocked=false (no incoming blocks edge).
        await ctx.proc('sp_Task_Depend', {
            p_milestone_id:     inProgress.milestone_id, p_task_no:     inProgress.task_no,
            p_dep_milestone_id: notStarted.milestone_id, p_dep_task_no: notStarted.task_no,
            p_dependency_verb:  'blocks',                p_reason:      'in-progress waits',
        });

        const rows = await ctx.kysely.selectFrom('vw_Task_Backlog')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .execute();

        // Only not-started + in-progress should be in backlog (done excluded).
        expect(rows.length).toBe(2);

        const taskNos = rows.map((r) => r.task_no).sort();
        expect(taskNos).toEqual([notStarted.task_no, inProgress.task_no].sort());

        const inProgressRow = rows.find((r) => r.task_no === inProgress.task_no);
        if (!inProgressRow) throw new Error('expected in-progress row');
        expect(inProgressRow.is_blocked).toBe(true);

        const notStartedRow = rows.find((r) => r.task_no === notStarted.task_no);
        if (!notStartedRow) throw new Error('expected not-started row');
        expect(notStartedRow.is_blocked).toBe(false);

        // Now soft-delete the milestone — its tasks should disappear from the backlog.
        await ctx.proc('sp_Milestone_Delete', {
            p_milestone_id: milestoneId, p_agent_id: 0, p_reason: 'archive',
        });

        const afterDelete = await ctx.kysely.selectFrom('vw_Task_Backlog')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .execute();

        expect(afterDelete.length).toBe(0);

    });

});

describe('vw_StateTransition', () => {

    it('populates the entity column matching the discriminator and leaves the rest as 0', async () => {

        // 1. Memory-relevance transition
        const memoryId = await makeMemory('st-mem');
        await ctx.proc('sp_Memory_SetRelevance', {
            p_memory_id: memoryId, p_new_relevance_status: 'needs-review',
            p_agent_id: 0, p_reason: 'r-mem',
        });

        // 2. Milestone-tracking transition (not-started -> in-progress)
        const milestoneId = await makeMilestone('st-ms');
        await ctx.proc('sp_Milestone_SetTracking', {
            p_milestone_id: milestoneId, p_new_tracking_status: 'in-progress',
            p_agent_id: 0, p_reason: 'r-ms',
        });

        // 3. Note-relevance transition
        const noteResult = await ctx.proc('sp_Note_Create_Project', {
            p_content: 'st-note', p_reason: '', p_provenance_id: 0, p_agent_id: 0, p_project_id: 0,
        });
        const [noteRow] = noteResult;
        if (!noteRow) throw new Error('note create failed');
        await ctx.proc('sp_Note_SetRelevance', {
            p_note_id: noteRow.note_id, p_new_relevance_status: 'needs-review',
            p_agent_id: 0, p_reason: 'r-note',
        });

        const memoryRow = await ctx.kysely.selectFrom('vw_StateTransition')
            .selectAll()
            .where('state_transition_type', '=', 'memory-relevance')
            .where('memory_id', '=', memoryId)
            .executeTakeFirstOrThrow();

        expect(memoryRow.memory_id).toBe(memoryId);
        expect(memoryRow.note_id).toBe(0);
        expect(memoryRow.artifact_id).toBe(0);
        expect(memoryRow.milestone_id).toBe(0);
        expect(memoryRow.task_no).toBe(0);
        expect(memoryRow.from_status).toBe('active');
        expect(memoryRow.to_status).toBe('needs-review');

        const milestoneRow = await ctx.kysely.selectFrom('vw_StateTransition')
            .selectAll()
            .where('state_transition_type', '=', 'milestone-tracking')
            .where('milestone_id', '=', milestoneId)
            .executeTakeFirstOrThrow();

        expect(milestoneRow.milestone_id).toBe(milestoneId);
        expect(milestoneRow.memory_id).toBe(0);
        expect(milestoneRow.note_id).toBe(0);
        expect(milestoneRow.artifact_id).toBe(0);
        expect(milestoneRow.task_no).toBe(0);
        expect(milestoneRow.from_status).toBe('not-started');
        expect(milestoneRow.to_status).toBe('in-progress');

        const noteStateRow = await ctx.kysely.selectFrom('vw_StateTransition')
            .selectAll()
            .where('state_transition_type', '=', 'note-relevance')
            .where('note_id', '=', noteRow.note_id)
            .executeTakeFirstOrThrow();

        expect(noteStateRow.note_id).toBe(noteRow.note_id);
        expect(noteStateRow.memory_id).toBe(0);
        expect(noteStateRow.artifact_id).toBe(0);
        expect(noteStateRow.milestone_id).toBe(0);
        expect(noteStateRow.task_no).toBe(0);

    });

});

describe('vw_Agent_Activity', () => {

    it('rolls up an agent\'s creates / transitions and last_action_at', async () => {

        const agentId = await makeAgent('busy');

        // 2 memories (and 1 transition on one of them)
        const m1 = await ctx.proc('sp_Memory_Create', {
            p_content: 'agent-mem-1', p_domain: 'backend', p_category: 'fact',
            p_reason: '', p_provenance_id: 0, p_agent_id: agentId,
            p_was_inferred: false, p_was_observed: true,
            p_was_evidenced: false, p_was_user_provided: false,
        });
        const [m1Row] = m1;
        if (!m1Row) throw new Error('mem 1 create failed');

        await ctx.proc('sp_Memory_Create', {
            p_content: 'agent-mem-2', p_domain: 'backend', p_category: 'fact',
            p_reason: '', p_provenance_id: 0, p_agent_id: agentId,
            p_was_inferred: false, p_was_observed: true,
            p_was_evidenced: false, p_was_user_provided: false,
        });

        // 1 note
        await ctx.proc('sp_Note_Create_Project', {
            p_content: 'agent-note', p_reason: '', p_provenance_id: 0,
            p_agent_id: agentId, p_project_id: 0,
        });

        // 1 transition (memory-relevance) attributed to agentId
        await ctx.proc('sp_Memory_SetRelevance', {
            p_memory_id:            m1Row.memory_id,
            p_new_relevance_status: 'needs-review',
            p_agent_id:             agentId,
            p_reason:               'flag',
        });

        const row = await ctx.kysely.selectFrom('vw_Agent_Activity')
            .selectAll()
            .where('agent_id', '=', agentId)
            .executeTakeFirstOrThrow();

        expect(row.memories_created).toBe(2);
        expect(row.notes_created).toBe(1);
        expect(row.artifacts_created).toBe(0);
        expect(row.milestones_created).toBe(0);
        expect(row.tasks_created).toBe(0);
        expect(row.tags_created).toBe(0);
        expect(row.transitions_made).toBe(1);
        expect(row.memories_superseded).toBe(0);
        // last_action_at should be the timestamp of the transition we made — far past 1970.
        expect(row.last_action_at.getTime()).toBeGreaterThan(new Date('2000-01-01').getTime());

    });

    it('shows zeros and the epoch sentinel for an agent with no activity', async () => {

        const agentId = await makeAgent('idle');

        const row = await ctx.kysely.selectFrom('vw_Agent_Activity')
            .selectAll()
            .where('agent_id', '=', agentId)
            .executeTakeFirstOrThrow();

        expect(row.memories_created).toBe(0);
        expect(row.notes_created).toBe(0);
        expect(row.transitions_made).toBe(0);
        // Epoch sentinel: 1970-01-01 (unix 0).
        expect(row.last_action_at.getTime()).toBe(0);

    });

});

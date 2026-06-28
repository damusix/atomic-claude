/**
 * View tests via ctx.kysely.selectFrom('vw_*').
 *
 * Seeds minimal data through procs, then reads back through the views to
 * assert shape + sensible projections. Views that aren't in the global DB
 * schema (Active_*, Deleted_*, Related_Memory, Recent_Activity,
 * Agent_Activity) are added inline via Kysely's withTables<...>() — we
 * never cast.
 */
import { beforeAll, beforeEach, describe, expect, it } from 'bun:test';

import { bootstrap, resetApplicationData } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});

beforeEach(async () => {

    await resetApplicationData(ctx);

});

// ──────────────────────────────────────────────────────────────
// View shapes for the views not declared in the global DB schema.
// ──────────────────────────────────────────────────────────────

interface ActiveMemoryView {
    memory_id: number;
    content: string;
    relevance_status: string;
    confidence: number;
}

interface ActiveNoteView {
    note_id: number;
    note_type: string;
    relevance_status: string;
    content: string;
}

interface ActiveArtifactView {
    artifact_id: number;
    relevance_status: string;
    title: string;
}

interface ActiveMilestoneView {
    milestone_id: number;
    tracking_status: string;
    relevance_status: string;
    title: string;
}

interface DeletedMemoryView {
    memory_id: number;
    content: string;
    relevance_status: string;
}

interface DeletedNoteView {
    note_id: number;
    relevance_status: string;
}

interface DeletedArtifactView {
    artifact_id: number;
    relevance_status: string;
}

interface DeletedMilestoneView {
    milestone_id: number;
    relevance_status: string;
}

interface ArtifactView {
    artifact_id: number;
    title: string;
    description: string;
    filepath: string;
    reason: string;
    relevance_status: string;
    provenance_id: number;
    relation_type: string;
    milestone_id: number;
    task_no: number;
    created_at: Date;
}

interface RelatedMemoryView {
    memory_id: number;
    related_memory_id: number;
    verb: string;
    reason: string;
    created_at: Date;
}

interface RecentActivityView {
    entity_type: string;
    entity_id: number;
    milestone_id: number;
    task_no: number;
    title_or_excerpt: string;
    agent_id: number;
    action_type: string;
    occurred_at: Date;
}

interface AgentActivityView {
    agent_id: number;
    name: string;
    memories_created: number;
    notes_created: number;
    artifacts_created: number;
    milestones_created: number;
    tasks_created: number;
    tags_created: number;
    transitions_made: number;
    memories_superseded: number;
    last_action_at: Date | null;
}

type ExtraViews = {
    vw_Active_Memory: ActiveMemoryView;
    vw_Active_Note: ActiveNoteView;
    vw_Active_Artifact: ActiveArtifactView;
    vw_Active_Milestone: ActiveMilestoneView;
    vw_Deleted_Memory: DeletedMemoryView;
    vw_Deleted_Note: DeletedNoteView;
    vw_Deleted_Artifact: DeletedArtifactView;
    vw_Deleted_Milestone: DeletedMilestoneView;
    vw_Artifact: ArtifactView;
    vw_Related_Memory: RelatedMemoryView;
    vw_Recent_Activity: RecentActivityView;
    vw_Agent_Activity: AgentActivityView;
};

// ──────────────────────────────────────────────────────────────
// Fixture builders
// ──────────────────────────────────────────────────────────────

async function makeAgent(name: string): Promise<number> {

    const rows = await ctx.proc('sp_Agent_Create', { name, description: '' });
    const created = rows[0];
    if (!created) throw new Error('agent create failed');
    return created.agent_id;

}

async function makeMemory(content: string, agentId = 0, evidenced = false): Promise<number> {

    const rows = await ctx.proc('sp_Memory_Create', {
        content, domain: 'backend', category: 'fact',
        reason: '', provenance_id: 0, agent_id: agentId,
        was_inferred: false, was_observed: true,
        was_evidenced: evidenced, was_user_provided: false,
    });
    const created = rows[0];
    if (!created) throw new Error('memory create failed');
    return created.memory_id;

}

async function makeMilestone(title = 'M', agentId = 0): Promise<number> {

    const rows = await ctx.proc('sp_Milestone_Create', {
        title, content: '', reason: '',
        provenance_id: 0, agent_id: agentId,
    });
    const created = rows[0];
    if (!created) throw new Error('milestone create failed');
    return created.milestone_id;

}

async function makeTask(milestoneId: number, title = 'T', agentId = 0): Promise<{ milestone_id: number; task_no: number }> {

    const rows = await ctx.proc('sp_Task_Create', {
        milestone_id: milestoneId, title, content: '',
        reason: '', agent_id: agentId,
    });
    const created = rows[0];
    if (!created) throw new Error('task create failed');
    return created;

}

async function makeArtifact(title = 'a', agentId = 0): Promise<number> {

    const rows = await ctx.proc('sp_Artifact_Create', {
        title, description: '', filepath: 'a.md',
        reason: '', provenance_id: 0, agent_id: agentId,
    });
    const created = rows[0];
    if (!created) throw new Error('artifact create failed');
    return created.artifact_id;

}

async function makeTag(name: string, agentId = 0): Promise<number> {

    const rows = await ctx.proc('sp_Tag_Create', {
        name, description: '', reason: '',
        provenance_id: 0, agent_id: agentId,
    });
    const created = rows[0];
    if (!created) throw new Error('tag create failed');
    return created.tag_id;

}

async function makeProjectNote(content: string, agentId = 0): Promise<number> {

    const rows = await ctx.proc('sp_Note_Create_Project', {
        content, reason: '', provenance_id: 0,
        agent_id: agentId, project_id: 0,
    });
    const created = rows[0];
    if (!created) throw new Error('note create failed');
    return created.note_id;

}

// ──────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────

describe('sql: vw_Tag', () => {

    it('emits one row per attachment with the correct relation_type discriminator', async () => {

        const tagId = await makeTag(`vw-tag-${Date.now()}`);
        const memId = await makeMemory('m');
        const milestoneId = await makeMilestone('Mst');

        await ctx.proc('sp_Tag_Attach_Memory', { tag_id: tagId, memory_id: memId });
        await ctx.proc('sp_Tag_Attach_Milestone', { tag_id: tagId, milestone_id: milestoneId });
        await ctx.proc('sp_Tag_Attach_Project', { tag_id: tagId, project_id: 0 });

        const rows = await ctx.kysely.selectFrom('vw_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .execute();

        expect(rows.length).toBe(3);

        const byType = new Map(rows.map((r) => [r.relation_type, r]));

        expect(byType.get('memory')?.memory_id).toBe(memId);
        expect(byType.get('milestone')?.milestone_id).toBe(milestoneId);
        expect(byType.get('project')?.project_id).toBe(0);

    });

});

describe('sql: vw_Artifact', () => {

    it('emits one row per attachment with the correct relation_type discriminator', async () => {

        const artifactId = await makeArtifact('vw-art');
        const milestoneId = await makeMilestone('vw-art-mst');
        const task = await makeTask(milestoneId, 'vw-art-task');

        await ctx.proc('sp_Artifact_Attach_Milestone', {
            artifact_id: artifactId, milestone_id: milestoneId,
        });
        await ctx.proc('sp_Artifact_Attach_Task', {
            artifact_id: artifactId,
            milestone_id: task.milestone_id, task_no: task.task_no,
        });

        const rows = await ctx.kysely
            .withTables<ExtraViews>()
            .selectFrom('vw_Artifact')
            .selectAll()
            .where('artifact_id', '=', artifactId)
            .execute();

        expect(rows.length).toBe(2);

        const byRelation = new Map(rows.map((r) => [r.relation_type, r]));

        const milestoneRow = byRelation.get('milestone');
        expect(milestoneRow?.milestone_id).toBe(milestoneId);
        expect(milestoneRow?.task_no).toBe(0);
        expect(milestoneRow?.title).toBe('vw-art');
        expect(milestoneRow?.relevance_status).toBe('active');

        const taskRow = byRelation.get('task');
        expect(taskRow?.milestone_id).toBe(task.milestone_id);
        expect(taskRow?.task_no).toBe(task.task_no);
        expect(taskRow?.title).toBe('vw-art');

    });

});

describe('sql: vw_Note', () => {

    it('LEFT JOINs into the correct subtype with COALESCE-zero defaults', async () => {

        const noteId = await makeProjectNote('a project note');

        const row = await ctx.kysely.selectFrom('vw_Note')
            .selectAll()
            .where('note_id', '=', noteId)
            .executeTakeFirstOrThrow();

        expect(row.note_type).toBe('project');
        expect(row.project_id).toBe(0);
        expect(row.milestone_id).toBe(0);
        expect(row.task_no).toBe(0);
        expect(row.content).toBe('a project note');

    });

});

describe('sql: vw_Memory', () => {

    it('exposes confidence equal to the count of true was_* booleans', async () => {

        const id = await makeMemory('with-conf', 0, true);
        // was_observed=true + was_evidenced=true => 2

        const row = await ctx.kysely.selectFrom('vw_Memory')
            .selectAll()
            .where('memory_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(row.confidence).toBe(2);

    });

});

describe('sql: vw_Related_Memory', () => {

    it('exposes both forward and backward directions for a stored edge', async () => {

        const a = await makeMemory('A');
        const b = await makeMemory('B');

        await ctx.proc('sp_Memory_Relate', {
            memory_id: a, related_memory_id: b,
            relation_verb: 'supersedes', reason: 'B replaces A',
        });

        const fromA = await ctx.kysely
            .withTables<ExtraViews>()
            .selectFrom('vw_Related_Memory')
            .selectAll()
            .where('memory_id', '=', a)
            .where('related_memory_id', '=', b)
            .executeTakeFirstOrThrow();

        expect(fromA.verb).toBe('supersedes');

        const fromB = await ctx.kysely
            .withTables<ExtraViews>()
            .selectFrom('vw_Related_Memory')
            .selectAll()
            .where('memory_id', '=', b)
            .where('related_memory_id', '=', a)
            .executeTakeFirstOrThrow();

        expect(fromB.verb).toBe('superseded-by');

    });

});

describe('sql: vw_Milestone_Stats', () => {

    it('aggregates total/done/abandoned/open task counts under the milestone', async () => {

        const id = await makeMilestone('stats');

        const t1 = await makeTask(id, 'open');
        const t2 = await makeTask(id, 'done');
        const t3 = await makeTask(id, 'abandoned');

        await ctx.proc('sp_Task_SetTracking', {
            milestone_id: t2.milestone_id, task_no: t2.task_no,
            new_tracking_status: 'in-progress', agent_id: 0, reason: '',
        });
        await ctx.proc('sp_Task_SetTracking', {
            milestone_id: t2.milestone_id, task_no: t2.task_no,
            new_tracking_status: 'done', agent_id: 0, reason: '',
        });

        await ctx.proc('sp_Task_SetTracking', {
            milestone_id: t3.milestone_id, task_no: t3.task_no,
            new_tracking_status: 'abandoned', agent_id: 0, reason: '',
        });

        const stats = await ctx.kysely.selectFrom('vw_Milestone_Stats')
            .selectAll()
            .where('milestone_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(stats.total_tasks).toBe(3);
        expect(stats.done_tasks).toBe(1);
        expect(stats.abandoned_tasks).toBe(1);
        // open = 1 (t1 still 'not-started')
        expect(stats.open_tasks).toBe(1);
        // hush unused-var hint for t1
        expect(t1.task_no).toBeGreaterThan(0);

    });

});

describe('sql: vw_Active_Memory', () => {

    it('only returns memories whose relevance_status = active', async () => {

        const stays = await makeMemory('stays-active');
        const leaves = await makeMemory('about-to-be-deleted');

        await ctx.proc('sp_Memory_Delete', { memory_id: leaves, agent_id: 0, reason: '' });

        const rows = await ctx.kysely
            .withTables<ExtraViews>()
            .selectFrom('vw_Active_Memory')
            .selectAll()
            .where('memory_id', 'in', [stays, leaves])
            .execute();

        const ids = rows.map((r) => r.memory_id);

        expect(ids).toContain(stays);
        expect(ids).not.toContain(leaves);
        for (const r of rows) expect(r.relevance_status).toBe('active');

    });

});

describe('sql: vw_Active_Note', () => {

    it('only returns notes whose relevance_status = active', async () => {

        const stays = await makeProjectNote('stays');
        const leaves = await makeProjectNote('leaves');

        await ctx.proc('sp_Note_Delete', { note_id: leaves, agent_id: 0, reason: '' });

        const rows = await ctx.kysely
            .withTables<ExtraViews>()
            .selectFrom('vw_Active_Note')
            .selectAll()
            .where('note_id', 'in', [stays, leaves])
            .execute();

        const ids = rows.map((r) => r.note_id);

        expect(ids).toContain(stays);
        expect(ids).not.toContain(leaves);
        for (const r of rows) expect(r.relevance_status).toBe('active');

    });

});

describe('sql: vw_Active_Artifact', () => {

    it('only returns artifacts whose relevance_status = active', async () => {

        const stays = await makeArtifact('stays');
        const leaves = await makeArtifact('leaves');

        await ctx.proc('sp_Artifact_Delete', { artifact_id: leaves, agent_id: 0, reason: '' });

        const rows = await ctx.kysely
            .withTables<ExtraViews>()
            .selectFrom('vw_Active_Artifact')
            .selectAll()
            .where('artifact_id', 'in', [stays, leaves])
            .execute();

        const ids = rows.map((r) => r.artifact_id);

        expect(ids).toContain(stays);
        expect(ids).not.toContain(leaves);
        for (const r of rows) expect(r.relevance_status).toBe('active');

    });

});

describe('sql: vw_Active_Milestone', () => {

    it('only returns milestones whose relevance_status = active', async () => {

        const stays = await makeMilestone('stays');
        const leaves = await makeMilestone('leaves');

        await ctx.proc('sp_Milestone_Delete', { milestone_id: leaves, agent_id: 0, reason: '' });

        const rows = await ctx.kysely
            .withTables<ExtraViews>()
            .selectFrom('vw_Active_Milestone')
            .selectAll()
            .where('milestone_id', 'in', [stays, leaves])
            .execute();

        const ids = rows.map((r) => r.milestone_id);

        expect(ids).toContain(stays);
        expect(ids).not.toContain(leaves);
        for (const r of rows) expect(r.relevance_status).toBe('active');

    });

});

describe('sql: vw_Deleted_Memory', () => {

    it('only returns memories whose relevance_status = deleted', async () => {

        const id = await makeMemory('soon-deleted');

        await ctx.proc('sp_Memory_Delete', { memory_id: id, agent_id: 0, reason: '' });

        const row = await ctx.kysely
            .withTables<ExtraViews>()
            .selectFrom('vw_Deleted_Memory')
            .selectAll()
            .where('memory_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(row.relevance_status).toBe('deleted');
        expect(row.content).toBe('soon-deleted');

    });

});

describe('sql: vw_Deleted_Note', () => {

    it('only returns notes whose relevance_status = deleted', async () => {

        const id = await makeProjectNote('soon-deleted');

        await ctx.proc('sp_Note_Delete', { note_id: id, agent_id: 0, reason: '' });

        const row = await ctx.kysely
            .withTables<ExtraViews>()
            .selectFrom('vw_Deleted_Note')
            .selectAll()
            .where('note_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(row.relevance_status).toBe('deleted');

    });

});

describe('sql: vw_Deleted_Artifact', () => {

    it('only returns artifacts whose relevance_status = deleted', async () => {

        const id = await makeArtifact('soon-deleted');

        await ctx.proc('sp_Artifact_Delete', { artifact_id: id, agent_id: 0, reason: '' });

        const row = await ctx.kysely
            .withTables<ExtraViews>()
            .selectFrom('vw_Deleted_Artifact')
            .selectAll()
            .where('artifact_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(row.relevance_status).toBe('deleted');

    });

});

describe('sql: vw_Deleted_Milestone', () => {

    it('only returns milestones whose relevance_status = deleted', async () => {

        const id = await makeMilestone('soon-deleted');

        await ctx.proc('sp_Milestone_Delete', { milestone_id: id, agent_id: 0, reason: '' });

        const row = await ctx.kysely
            .withTables<ExtraViews>()
            .selectFrom('vw_Deleted_Milestone')
            .selectAll()
            .where('milestone_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(row.relevance_status).toBe('deleted');

    });

});

describe('sql: vw_Task_Backlog', () => {

    it('lists open tasks under active milestones with the is_blocked flag', async () => {

        const milestoneId = await makeMilestone('backlog');

        const dependent = await makeTask(milestoneId, 'needs-dep');
        const blocker   = await makeTask(milestoneId, 'is-blocking');
        const standalone = await makeTask(milestoneId, 'free');

        // dependent depends on blocker via 'blocks'.
        await ctx.proc('sp_Task_Depend', {
            milestone_id:     dependent.milestone_id, task_no:     dependent.task_no,
            dep_milestone_id: blocker.milestone_id,   dep_task_no: blocker.task_no,
            dependency_verb: 'blocks', reason: 'b blocks a',
        });

        const rows = await ctx.kysely.selectFrom('vw_Task_Backlog')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .execute();

        expect(rows.length).toBe(3);

        const byNo = new Map(rows.map((r) => [r.task_no, r]));

        expect(byNo.get(dependent.task_no)?.is_blocked).toBe(true);
        expect(byNo.get(blocker.task_no)?.is_blocked).toBe(false);
        expect(byNo.get(standalone.task_no)?.is_blocked).toBe(false);

    });

});

describe('sql: vw_StateTransition', () => {

    it('LEFT JOINs to the correct subtype based on state_transition_type', async () => {

        const memId = await makeMemory('with-transition');

        await ctx.proc('sp_Memory_SetRelevance', {
            memory_id: memId, new_relevance_status: 'needs-review',
            agent_id: 0, reason: 'review pls',
        });

        const rows = await ctx.kysely.selectFrom('vw_StateTransition')
            .selectAll()
            .where('memory_id', '=', memId)
            .execute();

        expect(rows.length).toBeGreaterThan(0);

        const [row] = rows;

        if (!row) {

            throw new Error('expected a transition row');
        }

        expect(row.state_transition_type).toBe('memory-relevance');
        expect(row.from_status).toBe('active');
        expect(row.to_status).toBe('needs-review');
        // Other-entity columns COALESCE to 0.
        expect(row.task_no).toBe(0);
        expect(row.note_id).toBe(0);
        expect(row.artifact_id).toBe(0);

    });

});

describe('sql: vw_Recent_Activity', () => {

    it('UNIONs across created/updated/transitioned actions for many entity types', async () => {

        const stamp = Date.now();

        const memId = await makeMemory(`recent-${stamp}`);
        const milestoneId = await makeMilestone(`recent-ms-${stamp}`);

        await ctx.proc('sp_Memory_SetRelevance', {
            memory_id: memId, new_relevance_status: 'needs-review',
            agent_id: 0, reason: '',
        });

        const rows = await ctx.kysely
            .withTables<ExtraViews>()
            .selectFrom('vw_Recent_Activity')
            .selectAll()
            .where((eb) => eb.or([
                eb.and([eb('entity_type', '=', 'memory'), eb('entity_id', '=', memId)]),
                eb.and([eb('entity_type', '=', 'milestone'), eb('entity_id', '=', milestoneId)]),
                eb('entity_type', '=', 'transition'),
            ]))
            .execute();

        const types = new Set(rows.map((r) => r.entity_type));
        const actions = new Set(rows.map((r) => r.action_type));

        expect(types.has('memory')).toBe(true);
        expect(types.has('milestone')).toBe(true);
        expect(types.has('transition')).toBe(true);
        expect(actions.has('created')).toBe(true);
        expect(actions.has('transitioned')).toBe(true);

    });

});

describe('sql: vw_Agent_Activity', () => {

    it('rolls up created counts and transitions per agent', async () => {

        const agentId = await makeAgent(`agent-act-${Date.now()}`);

        const m1 = await makeMemory('a-m1', agentId);
        await makeMemory('a-m2', agentId);
        await makeMilestone('a-ms', agentId);

        await ctx.proc('sp_Memory_SetRelevance', {
            memory_id: m1, new_relevance_status: 'needs-review',
            agent_id: agentId, reason: '',
        });

        const row = await ctx.kysely
            .withTables<ExtraViews>()
            .selectFrom('vw_Agent_Activity')
            .selectAll()
            .where('agent_id', '=', agentId)
            .executeTakeFirstOrThrow();

        expect(row.memories_created).toBe(2);
        expect(row.milestones_created).toBe(1);
        expect(row.transitions_made).toBeGreaterThanOrEqual(1);

    });

});

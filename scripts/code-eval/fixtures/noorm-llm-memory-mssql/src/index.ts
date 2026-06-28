/**
 * LlmMemoryDb — public facade. Each domain exposes `cmd` (DML through
 * stored procs) and `qry` (DQL through Kysely / views / functions /
 * TVFs). The audit domain is read-only and only exposes `qry`.
 *
 * @example
 * const ctx = await createMemoryDbContext({ config: 'dev' });
 * await ctx.connect();
 * const db = new LlmMemoryDb(ctx);
 * await db.tag.cmd.bulkAttachMemory({ pairs: [{ tagId: 1, memoryId: 10 }] });
 * const memories = await db.tag.qry.filterMemoriesByTags({ tagIds: [1, 2, 3] });
 */
import { AgentCommands, AgentQueries } from './agent';
import { ArtifactCommands, ArtifactQueries } from './artifact';
import { AuditQueries } from './audit';
import { MemoryCommands, MemoryQueries } from './memory';
import { MilestoneCommands, MilestoneQueries } from './milestone';
import { NoteCommands, NoteQueries } from './note';
import { ProjectCommands, ProjectQueries } from './project';
import { TagCommands, TagQueries } from './tag';
import { TaskCommands, TaskQueries } from './task';

import type { MemoryDbContext } from './core/context';

export class LlmMemoryDb {

    readonly agent: { cmd: AgentCommands; qry: AgentQueries };
    readonly project: { cmd: ProjectCommands; qry: ProjectQueries };
    readonly memory: { cmd: MemoryCommands; qry: MemoryQueries };
    readonly note: { cmd: NoteCommands; qry: NoteQueries };
    readonly tag: { cmd: TagCommands; qry: TagQueries };
    readonly artifact: { cmd: ArtifactCommands; qry: ArtifactQueries };
    readonly milestone: { cmd: MilestoneCommands; qry: MilestoneQueries };
    readonly task: { cmd: TaskCommands; qry: TaskQueries };
    readonly audit: { qry: AuditQueries };

    constructor(ctx: MemoryDbContext) {

        this.agent = { cmd: new AgentCommands(ctx), qry: new AgentQueries(ctx) };
        this.project = { cmd: new ProjectCommands(ctx), qry: new ProjectQueries(ctx) };
        this.memory = { cmd: new MemoryCommands(ctx), qry: new MemoryQueries(ctx) };
        this.note = { cmd: new NoteCommands(ctx), qry: new NoteQueries(ctx) };
        this.tag = { cmd: new TagCommands(ctx), qry: new TagQueries(ctx) };
        this.artifact = { cmd: new ArtifactCommands(ctx), qry: new ArtifactQueries(ctx) };
        this.milestone = { cmd: new MilestoneCommands(ctx), qry: new MilestoneQueries(ctx) };
        this.task = { cmd: new TaskCommands(ctx), qry: new TaskQueries(ctx) };
        this.audit = { qry: new AuditQueries(ctx) };

    }

}

export { createMemoryDbContext } from './core/context';
export type { MemoryDbContext } from './core/context';
export type { DB, Procs, Funcs, Tvfs } from './core/types';

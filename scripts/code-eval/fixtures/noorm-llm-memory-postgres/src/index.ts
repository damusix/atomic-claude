import { AgentCommands, AgentQueries } from './agent';
import { ArtifactCommands, ArtifactQueries } from './artifact';
import { AuditQueries } from './audit';
import { MemoryCommands, MemoryQueries } from './memory';
import { MilestoneCommands, MilestoneQueries } from './milestone';
import { NoteCommands, NoteQueries } from './note';
import { ProjectCommands, ProjectQueries } from './project';
import { TagCommands, TagQueries } from './tag';
import { TaskCommands, TaskQueries } from './task';

import type { Context, DB, Procs, Funcs, Tvfs } from './core';

/**
 * Public facade — one cmd/qry pair per domain.
 *
 * @example
 * ```typescript
 * const ctx = await createContext({ config: 'dev' });
 * await ctx.connect();
 * const db = new LlmMemoryDb(ctx);
 *
 * const memoryId = await db.memory.cmd.create({ content: '...', ... });
 * const memory = await db.memory.qry.findById(memoryId);
 * await ctx.disconnect();
 * ```
 */
export class LlmMemoryDb {

    readonly agent:     { cmd: AgentCommands;     qry: AgentQueries };
    readonly project:   { cmd: ProjectCommands;   qry: ProjectQueries };
    readonly memory:    { cmd: MemoryCommands;    qry: MemoryQueries };
    readonly note:      { cmd: NoteCommands;      qry: NoteQueries };
    readonly tag:       { cmd: TagCommands;       qry: TagQueries };
    readonly artifact:  { cmd: ArtifactCommands;  qry: ArtifactQueries };
    readonly milestone: { cmd: MilestoneCommands; qry: MilestoneQueries };
    readonly task:      { cmd: TaskCommands;      qry: TaskQueries };
    readonly audit:     { qry: AuditQueries };

    constructor(ctx: Context<DB, Procs, Funcs, Tvfs>) {

        this.agent     = { cmd: new AgentCommands(ctx),     qry: new AgentQueries(ctx) };
        this.project   = { cmd: new ProjectCommands(ctx),   qry: new ProjectQueries(ctx) };
        this.memory    = { cmd: new MemoryCommands(ctx),    qry: new MemoryQueries(ctx) };
        this.note      = { cmd: new NoteCommands(ctx),      qry: new NoteQueries(ctx) };
        this.tag       = { cmd: new TagCommands(ctx),       qry: new TagQueries(ctx) };
        this.artifact  = { cmd: new ArtifactCommands(ctx),  qry: new ArtifactQueries(ctx) };
        this.milestone = { cmd: new MilestoneCommands(ctx), qry: new MilestoneQueries(ctx) };
        this.task      = { cmd: new TaskCommands(ctx),      qry: new TaskQueries(ctx) };
        this.audit     = { qry: new AuditQueries(ctx) };

    }

}

// Re-exports — callers consume one symbol from this barrel and the public surface comes with it.
export { createContext } from './core';
export type { Context, DB, Procs, Funcs, Tvfs } from './core';

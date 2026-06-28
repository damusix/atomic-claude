import { z } from 'zod';

import { Repo } from '../core/repo';

/**
 * Options accepted by `TagQueries.list`.
 *
 * Inlined here (rather than living in `schema.ts`) because list options are
 * read-side concerns that don't map onto a stored procedure. Defaults
 * follow the project-wide convention: 50-row pages capped at 500.
 */
const ListTagOpts = z.object({
    limit:  z.number().int().positive().max(500).default(50),
    offset: z.number().int().nonnegative().default(0),
});

/**
 * Read surface for the `Tag` domain.
 *
 * Direct lookups (`findById`, `findByName`) hit the raw `Tag` table.
 * Attachment-aware reads (`listAttachments`, `listFor*`) hit the
 * polymorphic `vw_Tag` view, which UNIONs the five `*_Tag` join tables and
 * surfaces the discriminator as `relation_type`. A tag with N attachments
 * appears N times in `vw_Tag`; a tag with no attachments doesn't appear at
 * all (inner-join semantics).
 *
 * @example
 * ```typescript
 * const tag        = await db.tag.qry.findById(7);
 * const named      = await db.tag.qry.findByName('backend-perf');
 * const page       = await db.tag.qry.list({ limit: 25 });
 * const allAttach  = await db.tag.qry.listAttachments(7);
 * const projectTag = await db.tag.qry.listForProject(1);
 * ```
 */
export class TagQueries extends Repo {

    /**
     * Look up a single Tag by its primary key.
     *
     * Returns `undefined` when no row matches — callers decide whether
     * that's an error or a normal "not yet created" signal.
     *
     * @example
     * ```typescript
     * const tag = await db.tag.qry.findById(7);
     * ```
     */
    async findById(tagId: number) {

        return this.ctx.kysely
            .selectFrom('Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .executeTakeFirst();

    }

    /**
     * Look up a Tag by its unique `name`. The column has a UNIQUE
     * constraint, so at most one row matches.
     *
     * @example
     * ```typescript
     * const tag = await db.tag.qry.findByName('backend-perf');
     * ```
     */
    async findByName(name: string) {

        return this.ctx.kysely
            .selectFrom('Tag')
            .selectAll()
            .where('name', '=', name)
            .executeTakeFirst();

    }

    /**
     * Paginate Tags ordered alphabetically by `name`. Useful for picker UIs
     * where the user scrolls a registry of known tags.
     *
     * @example
     * ```typescript
     * const firstPage  = await db.tag.qry.list({ limit: 25 });
     * const secondPage = await db.tag.qry.list({ limit: 25, offset: 25 });
     * ```
     */
    async list(input: unknown) {

        const opts = ListTagOpts.parse(input);

        return this.ctx.kysely
            .selectFrom('Tag')
            .selectAll()
            .orderBy('name', 'asc')
            .limit(opts.limit)
            .offset(opts.offset)
            .execute();

    }

    /**
     * List every attachment of a single tag, across all five entity kinds.
     *
     * Each row carries `relation_type` (`project` | `memory` | `artifact` |
     * `milestone` | `task`) plus the matching entity-id column populated;
     * unused entity columns are zero-filled.
     *
     * @example
     * ```typescript
     * const rows = await db.tag.qry.listAttachments(7);
     * for (const row of rows) {
     *     console.log(row.relation_type, row.project_id || row.memory_id);
     * }
     * ```
     */
    async listAttachments(tagId: number) {

        return this.ctx.kysely
            .selectFrom('vw_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .execute();

    }

    /**
     * List every Tag attached to a given Project.
     *
     * Filters `vw_Tag` by `relation_type = 'project'` AND the matching
     * `project_id`, so two Tags on the same Project produce two rows
     * (one per attachment).
     *
     * @example
     * ```typescript
     * const tags = await db.tag.qry.listForProject(1);
     * ```
     */
    async listForProject(projectId: number) {

        return this.ctx.kysely
            .selectFrom('vw_Tag')
            .selectAll()
            .where('relation_type', '=', 'project')
            .where('project_id', '=', projectId)
            .execute();

    }

    /**
     * List every Tag attached to a given Memory.
     *
     * @example
     * ```typescript
     * const tags = await db.tag.qry.listForMemory(42);
     * ```
     */
    async listForMemory(memoryId: number) {

        return this.ctx.kysely
            .selectFrom('vw_Tag')
            .selectAll()
            .where('relation_type', '=', 'memory')
            .where('memory_id', '=', memoryId)
            .execute();

    }

    /**
     * List every Tag attached to a given Artifact.
     *
     * @example
     * ```typescript
     * const tags = await db.tag.qry.listForArtifact(99);
     * ```
     */
    async listForArtifact(artifactId: number) {

        return this.ctx.kysely
            .selectFrom('vw_Tag')
            .selectAll()
            .where('relation_type', '=', 'artifact')
            .where('artifact_id', '=', artifactId)
            .execute();

    }

    /**
     * List every Tag attached to a given Milestone.
     *
     * Note: this returns Tags attached *directly* to the milestone — Tags on
     * the milestone's tasks come back via `listForTask` keyed by the same
     * `milestoneId` plus the task's `taskNo`.
     *
     * @example
     * ```typescript
     * const tags = await db.tag.qry.listForMilestone(3);
     * ```
     */
    async listForMilestone(milestoneId: number) {

        return this.ctx.kysely
            .selectFrom('vw_Tag')
            .selectAll()
            .where('relation_type', '=', 'milestone')
            .where('milestone_id', '=', milestoneId)
            .execute();

    }

    /**
     * List every Tag attached to a given Task. Tasks have a composite PK,
     * so both `milestoneId` and `taskNo` are required.
     *
     * @example
     * ```typescript
     * const tags = await db.tag.qry.listForTask(3, 12);
     * ```
     */
    async listForTask(milestoneId: number, taskNo: number) {

        return this.ctx.kysely
            .selectFrom('vw_Tag')
            .selectAll()
            .where('relation_type', '=', 'task')
            .where('milestone_id', '=', milestoneId)
            .where('task_no', '=', taskNo)
            .execute();

    }

}

import { Repo } from '../core/repo';

import { ListArtifactOpts } from './schema';

/**
 * Read-side API for the `Artifact` domain.
 *
 * Lookups by id read directly from the `Artifact` table; the active /
 * deleted list helpers read from the relevance-filtered views
 * (`vw_Active_Artifact`, `vw_Deleted_Artifact`) so callers don't need to
 * remember the discriminator literal. The `listForMilestone` /
 * `listForTask` helpers query `vw_Artifact`, which UNION-ALLs both join
 * tables and surfaces the discriminator via `relation_type`.
 *
 * All list methods are pagination-bounded via {@link ListArtifactOpts}.
 *
 * @example
 * ```typescript
 * const artifact = await db.artifact.q.findById(1);
 * const active   = await db.artifact.q.listActive({ limit: 20 });
 * const onM1     = await db.artifact.q.listForMilestone(1);
 * ```
 */
export class ArtifactQueries extends Repo {

    /**
     * Look an Artifact up by its primary key. Returns `undefined` when
     * the id does not match a row.
     *
     * @example
     * ```typescript
     * const artifact = await db.artifact.q.findById(1);
     * ```
     */
    async findById(artifactId: number) {

        return this.ctx.kysely
            .selectFrom('Artifact')
            .selectAll()
            .where('artifact_id', '=', artifactId)
            .executeTakeFirst();

    }

    /**
     * Paginate Artifacts whose `relevance_status = 'active'`. Reads from
     * `vw_Active_Artifact` so the filter literal lives in SQL, not here.
     *
     * @example
     * ```typescript
     * const firstPage  = await db.artifact.q.listActive({ limit: 20 });
     * const secondPage = await db.artifact.q.listActive({ limit: 20, offset: 20 });
     * ```
     */
    async listActive(input: unknown) {

        const opts = ListArtifactOpts.parse(input);

        return this.ctx.kysely
            .selectFrom('vw_Active_Artifact')
            .selectAll()
            .orderBy('created_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset)
            .execute();

    }

    /**
     * Paginate Artifacts whose `relevance_status = 'deleted'`. Useful for
     * "trash" / restore UIs. Reads from `vw_Deleted_Artifact`.
     *
     * @example
     * ```typescript
     * const trash = await db.artifact.q.listDeleted({ limit: 50 });
     * ```
     */
    async listDeleted(input: unknown) {

        const opts = ListArtifactOpts.parse(input);

        return this.ctx.kysely
            .selectFrom('vw_Deleted_Artifact')
            .selectAll()
            .orderBy('updated_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset)
            .execute();

    }

    /**
     * List every Artifact attached to a given Milestone via
     * `Milestone_Artifact`. Reads from `vw_Artifact` (the polymorphic
     * artifact-attachment view) filtered to `relation_type = 'milestone'`.
     *
     * @example
     * ```typescript
     * const onM1 = await db.artifact.q.listForMilestone(1);
     * ```
     */
    async listForMilestone(milestoneId: number) {

        return this.ctx.kysely
            .selectFrom('vw_Artifact')
            .selectAll()
            .where('relation_type', '=', 'milestone')
            .where('milestone_id', '=', milestoneId)
            .orderBy('created_at', 'desc')
            .execute();

    }

    /**
     * List every Artifact attached to a given Task via `Task_Artifact`.
     * Tasks are keyed by (`milestoneId`, `taskNo`); reads from
     * `vw_Artifact` filtered to `relation_type = 'task'`.
     *
     * @example
     * ```typescript
     * const onTask = await db.artifact.q.listForTask(1, 3);
     * ```
     */
    async listForTask(milestoneId: number, taskNo: number) {

        return this.ctx.kysely
            .selectFrom('vw_Artifact')
            .selectAll()
            .where('relation_type', '=', 'task')
            .where('milestone_id', '=', milestoneId)
            .where('task_no', '=', taskNo)
            .orderBy('created_at', 'desc')
            .execute();

    }

}

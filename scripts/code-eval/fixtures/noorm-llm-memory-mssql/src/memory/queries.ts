/**
 * DQL for the Memory domain.
 *
 * Read paths use Kysely for table/view selects and ctx.func for scalar
 * UDFs. vw_Memory is typed in core/types.ts; vw_Active_Memory and
 * vw_Related_Memory are extended in here via Kysely's withTables() so
 * we don't have to grow the global DB schema for views that only this
 * domain reads.
 *
 * @example
 * const memories = new MemoryQueries(ctx);
 * const enriched = await memories.findByIdWithConfidence(42);
 * const score = await memories.rank(42);
 */
import { Repo } from '../core/repo';

import type { MemoryView } from './types';

/** Symmetric Related_Memory projection — see sql/08_views/04_vw_Related_Memory.sql. */
interface RelatedMemoryView {
    memory_id: number;
    related_memory_id: number;
    verb: string;
    reason: string;
    created_at: Date;
}

/** Local extensions used only by this query class. */
type MemoryQueriesViews = {
    vw_Active_Memory: MemoryView;
    vw_Related_Memory: RelatedMemoryView;
};

export class MemoryQueries extends Repo {

    /** Fetch a single Memory row by id, or undefined when no row matches. */
    async findById(memoryId: number) {

        return this.ctx.kysely
            .selectFrom('Memory')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .executeTakeFirst();

    }

    /** Fetch a Memory through vw_Memory so the computed confidence is included. */
    async findByIdWithConfidence(memoryId: number) {

        return this.ctx.kysely
            .selectFrom('vw_Memory')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .executeTakeFirst();

    }

    /** Scalar fn_MemoryConfidence — count of true was_* booleans (0-4). */
    async confidence(memoryId: number) {

        return this.ctx.func(
            'fn_MemoryConfidence',
            { memory_id: memoryId },
            'confidence',
        );

    }

    /** Scalar fn_MemoryRank — composite retrieval score (0.0-1.0). */
    async rank(memoryId: number) {

        return this.ctx.func(
            'fn_MemoryRank',
            { memory_id: memoryId },
            'rank',
        );

    }

    /** All currently-active memories (vw_Active_Memory = vw_Memory filtered). */
    async listActive() {

        return this.ctx.kysely
            .withTables<MemoryQueriesViews>()
            .selectFrom('vw_Active_Memory')
            .selectAll()
            .execute();

    }

    /**
     * All relations for a memory in symmetric form (verb_backward applied
     * when this memory is on the right-hand side of the stored row).
     */
    async listRelated(memoryId: number) {

        return this.ctx.kysely
            .withTables<MemoryQueriesViews>()
            .selectFrom('vw_Related_Memory')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .execute();

    }

}

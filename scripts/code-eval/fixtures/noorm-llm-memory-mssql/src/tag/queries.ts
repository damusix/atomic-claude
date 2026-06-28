/**
 * DQL for the Tag domain.
 *
 * Read paths use Kysely directly so callers can compose narrower
 * projections when they need them. The vw_Tag-backed helpers expose the
 * inclusive-subtype attachment graph; the relational-division TVF
 * (filterMemoriesByTags) returns memories carrying every supplied tag.
 *
 * @example
 * const tags = new TagQueries(ctx);
 * const all = await tags.list();
 * const attachments = await tags.listAttachments(1);
 * const matched = await tags.filterMemoriesByTags({ tagIds: [1, 2, 3] });
 */
import { tvp } from '@noormdev/sdk';

import { Repo } from '../core/repo';

import { FilterMemoriesByTagsInput } from './schema';

export class TagQueries extends Repo {

    /** Fetch a single base Tag row by id, or undefined when no row matches. */
    async findById(tagId: number) {

        return this.ctx.kysely
            .selectFrom('Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .executeTakeFirst();

    }

    /** Fetch a single base Tag row by name (UQ_Tag_name guarantees uniqueness). */
    async findByName(name: string) {

        return this.ctx.kysely
            .selectFrom('Tag')
            .selectAll()
            .where('name', '=', name)
            .executeTakeFirst();

    }

    /** List every Tag ordered by name (case-insensitive collation in SQL). */
    async list() {

        return this.ctx.kysely
            .selectFrom('Tag')
            .selectAll()
            .orderBy('name', 'asc')
            .execute();

    }

    /** List every (tag, attached entity) pair via vw_Tag, optionally for one tag. */
    async listAttachments(tagId?: number) {

        let q = this.ctx.kysely
            .selectFrom('vw_Tag')
            .selectAll();

        if (tagId !== undefined) {

            q = q.where('tag_id', '=', tagId);
        }

        return q.orderBy('tag_id', 'asc').execute();

    }

    /**
     * Return memories carrying every supplied tag, ranked by fn_MemoryRank.
     * Uses the relational-division TVF; an empty tag list rejects upfront
     * via Zod rather than producing a vacuous full-table result.
     */
    async filterMemoriesByTags(input: unknown) {

        const { tagIds } = FilterMemoriesByTagsInput.parse(input);

        return this.ctx.tvf('tvf_FilterMemoriesByTags', {
            TagIds: tvp('TagIdSet', tagIds.map((id) => ({ tag_id: id }))),
        });

    }

}

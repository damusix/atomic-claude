import type { Context } from '@noormdev/sdk';

import type { DB, Procs, Funcs, Tvfs } from './types';

/**
 * Base class for every domain's commands and queries.
 *
 * Eliminates the repetitive four-generic `Context<DB, Procs, Funcs, Tvfs>`
 * declaration so domain classes can focus on their actual operations.
 *
 * @example
 * ```typescript
 * export class MemoryCommands extends Repo {
 *
 *     async create(input: unknown): Promise<number> {
 *
 *         const row = await this.ctx.kysely
 *             .insertInto('Memory')
 *             .values(input)
 *             .returning('memory_id')
 *             .executeTakeFirstOrThrow();
 *
 *         return row.memory_id;
 *
 *     }
 *
 * }
 * ```
 */
export abstract class Repo {

    constructor(protected readonly ctx: Context<DB, Procs, Funcs, Tvfs>) {}

}

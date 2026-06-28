/**
 * Base class for every domain repo (commands and queries).
 *
 * Both *Commands and *Queries extend this so each domain doesn't have
 * to re-declare the four-generic Context type parameter list per file.
 *
 * @example
 * export class TagCommands extends Repo {
 *     async create(input: unknown) {
 *         return this.ctx.proc('sp_Tag_Create', ...);
 *     }
 * }
 */
import type { Context } from '@noormdev/sdk';

import type { DB, Funcs, Procs, Tvfs } from './types';

export abstract class Repo {

    constructor(protected readonly ctx: Context<DB, Procs, Funcs, Tvfs>) {}

}

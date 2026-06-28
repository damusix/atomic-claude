import {
    createContext as sdkCreateContext,
    type Context,
    type CreateContextOptions,
} from '@noormdev/sdk';

import type { DB, Procs, Funcs, Tvfs } from './types';

/**
 * Project-typed `createContext` — pins the SDK's four generics to the
 * LLM-memory schema so callers never have to re-declare them.
 *
 * Use this in place of `@noormdev/sdk`'s raw `createContext` everywhere
 * inside this example. Tests and CLI entry points get full Kysely
 * autocomplete on every table, view, proc, and function for free.
 *
 * @example
 * ```typescript
 * const ctx = await createContext({ config: 'dev' });
 *
 * await ctx.connect();
 *
 * const memories = await ctx.kysely
 *     .selectFrom('vw_Active_Memory')
 *     .selectAll()
 *     .execute();
 * ```
 */
export async function createContext(
    options?: CreateContextOptions,
): Promise<Context<DB, Procs, Funcs, Tvfs>> {

    return sdkCreateContext<DB, Procs, Funcs, Tvfs>(options);

}

export type { Context, CreateContextOptions };

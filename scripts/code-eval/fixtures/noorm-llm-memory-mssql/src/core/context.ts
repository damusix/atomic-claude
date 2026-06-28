/**
 * Strongly-typed createContext factory for this example.
 *
 * Re-exports the SDK factory bound to this project's DB/Procs/Funcs/Tvfs
 * generics so call sites and tests don't have to repeat the four-tuple.
 *
 * @example
 * const ctx = await createMemoryDbContext({ config: 'dev' });
 * await ctx.connect();
 */
import { createContext, type Context, type CreateContextOptions } from '@noormdev/sdk';

import type { DB, Funcs, Procs, Tvfs } from './types';

export type MemoryDbContext = Context<DB, Procs, Funcs, Tvfs>;

export async function createMemoryDbContext(
    options: CreateContextOptions = {},
): Promise<MemoryDbContext> {

    return createContext<DB, Procs, Funcs, Tvfs>(options);

}

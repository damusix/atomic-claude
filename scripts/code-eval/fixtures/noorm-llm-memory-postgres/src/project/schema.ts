import { z } from 'zod';

/**
 * Input for `ProjectCommands.create`.
 *
 * `name` is the only field that must be supplied; every other VARCHAR
 * mirrors the table's `DEFAULT ''` and the proc accepts an empty string.
 * `agentId` records who registered the project — pass `0` for the
 * "no attribution" sentinel agent.
 *
 * @example
 * ```typescript
 * const args = CreateProjectInput.parse({ name: 'noorm', agentId: 1 });
 * ```
 */
export const CreateProjectInput = z.object({
    name:       z.string().min(1),
    filepath:   z.string().default(''),
    gitRepo:    z.string().default(''),
    mainBranch: z.string().default(''),
    gitUrl:     z.string().default(''),
    agentId:    z.number().int().nonnegative(),
});
export type CreateProjectInput = z.infer<typeof CreateProjectInput>;

/**
 * Input for `ProjectCommands.update`.
 *
 * `sp_Project_Update` rewrites every text column unconditionally — there
 * is no partial-update path in the proc — so the caller is responsible
 * for passing the canonical (already-merged) values for every field.
 *
 * @example
 * ```typescript
 * const args = UpdateProjectInput.parse({
 *     projectId: 1, name: 'noorm', filepath: '/repos/noorm',
 *     gitRepo: 'noormdev/noorm', mainBranch: 'master',
 *     gitUrl: 'git@github.com:noormdev/noorm.git',
 * });
 * ```
 */
export const UpdateProjectInput = z.object({
    projectId:  z.number().int().positive(),
    name:       z.string().min(1),
    filepath:   z.string(),
    gitRepo:    z.string(),
    mainBranch: z.string(),
    gitUrl:     z.string(),
});
export type UpdateProjectInput = z.infer<typeof UpdateProjectInput>;

/**
 * Input for `ProjectCommands.softDelete`.
 *
 * `sp_Project_Delete` is a hard delete at the SQL layer (it reassigns
 * provenance to the sentinel project then drops the row); the JS-side
 * method is named `softDelete` only to dodge `delete` as a reserved word.
 *
 * @example
 * ```typescript
 * const args = DeleteProjectInput.parse({ projectId: 1 });
 * ```
 */
export const DeleteProjectInput = z.object({
    projectId: z.number().int().positive(),
});
export type DeleteProjectInput = z.infer<typeof DeleteProjectInput>;

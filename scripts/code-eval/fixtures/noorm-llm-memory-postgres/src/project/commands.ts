import { Repo } from '../core/repo';

import {
    CreateProjectInput,
    UpdateProjectInput,
    DeleteProjectInput,
} from './schema';

/**
 * Write-side API for the `Project` table.
 *
 * Every mutation is funneled through the matching `sp_Project_*` proc so the
 * sentinel-row guards, FK cascades, and provenance reassignment defined in
 * SQL stay authoritative. Methods accept `unknown` and validate at the
 * boundary with Zod; downstream code reads the parsed, camelCase result.
 *
 * @example
 * ```typescript
 * const projectId = await db.project.cmd.create({ name: 'noorm', agentId: 1 });
 * await db.project.cmd.update({
 *     projectId, name: 'noorm', filepath: '/repos/noorm',
 *     gitRepo: 'noormdev/noorm', mainBranch: 'master',
 *     gitUrl: 'git@github.com:noormdev/noorm.git',
 * });
 * await db.project.cmd.softDelete({ projectId });
 * ```
 */
export class ProjectCommands extends Repo {

    /**
     * Register a new Project and return its generated id.
     *
     * @example
     * ```typescript
     * const projectId = await db.project.cmd.create({
     *     name: 'noorm', filepath: '/repos/noorm',
     *     gitRepo: 'noormdev/noorm', mainBranch: 'master',
     *     gitUrl: 'git@github.com:noormdev/noorm.git',
     *     agentId: 1,
     * });
     * ```
     */
    async create(input: unknown): Promise<number> {

        const args = CreateProjectInput.parse(input);

        const [row] = await this.ctx.proc('sp_Project_Create', {
            p_name:        args.name,
            p_filepath:    args.filepath,
            p_git_repo:    args.gitRepo,
            p_main_branch: args.mainBranch,
            p_git_url:     args.gitUrl,
            p_agent_id:    args.agentId,
        });

        if (!row) throw new Error('sp_Project_Create returned no rows');

        return row.project_id;

    }

    /**
     * Overwrite a Project's mutable columns. The proc rejects `project_id = 0`
     * (the sentinel "none" project is immutable).
     *
     * @example
     * ```typescript
     * await db.project.cmd.update({
     *     projectId: 1, name: 'noorm', filepath: '/repos/noorm',
     *     gitRepo: 'noormdev/noorm', mainBranch: 'master',
     *     gitUrl: 'git@github.com:noormdev/noorm.git',
     * });
     * ```
     */
    async update(input: unknown): Promise<void> {

        const args = UpdateProjectInput.parse(input);

        await this.ctx.proc('sp_Project_Update', {
            p_project_id:  args.projectId,
            p_name:        args.name,
            p_filepath:    args.filepath,
            p_git_repo:    args.gitRepo,
            p_main_branch: args.mainBranch,
            p_git_url:     args.gitUrl,
        });

    }

    /**
     * Hard-delete a Project after reassigning provenance to the sentinel
     * project (id `0`). FK `ON DELETE CASCADE` removes the join rows
     * (`Project_Tag`, `Project_Memory`, `Project_Milestone`, `Project_Note`).
     * Named `softDelete` only to avoid `delete` as a method name in TS.
     *
     * @example
     * ```typescript
     * await db.project.cmd.softDelete({ projectId: 1 });
     * ```
     */
    async softDelete(input: unknown): Promise<void> {

        const args = DeleteProjectInput.parse(input);

        await this.ctx.proc('sp_Project_Delete', {
            p_project_id: args.projectId,
        });

    }

}

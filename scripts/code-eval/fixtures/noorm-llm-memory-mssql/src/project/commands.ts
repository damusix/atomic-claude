/**
 * DML for the Project domain.
 *
 * Methods validate with Zod, then dispatch to the matching stored proc.
 * Sentinel-id protection (Project(0)) and the cascade-soft-delete of
 * attached Notes both live in the proc — we let RAISERROR propagate.
 *
 * @example
 * const projects = new ProjectCommands(ctx);
 * const { project_id } = await projects.create({ name: 'noorm' });
 */
import { Repo } from '../core/repo';

import {
    CreateProjectInput,
    DeleteProjectInput,
    UpdateProjectInput,
} from './schema';

export class ProjectCommands extends Repo {

    /** Insert a new Project (optionally bound to an Agent) and return its id. */
    async create(input: unknown): Promise<{ project_id: number }> {

        const parsed = CreateProjectInput.parse(input);

        const rows = await this.ctx.proc('sp_Project_Create', {
            name: parsed.name,
            filepath: parsed.filepath,
            git_repo: parsed.gitRepo,
            main_branch: parsed.mainBranch,
            git_url: parsed.gitUrl,
            agent_id: parsed.agentId,
        });

        const row = rows[0];
        if (!row) throw new Error('sp_Project_Create returned no rows.');

        return row;

    }

    /** Update Project metadata (sentinel 0 rejected by proc). */
    async update(input: unknown) {

        const parsed = UpdateProjectInput.parse(input);

        return this.ctx.proc('sp_Project_Update', {
            project_id: parsed.projectId,
            name: parsed.name,
            filepath: parsed.filepath,
            git_repo: parsed.gitRepo,
            main_branch: parsed.mainBranch,
            git_url: parsed.gitUrl,
        });

    }

    /** Reassign provenance to 0, soft-delete attached Notes, then drop the row. */
    async delete(input: unknown) {

        const parsed = DeleteProjectInput.parse(input);

        return this.ctx.proc('sp_Project_Delete', {
            project_id: parsed.projectId,
        });

    }

}

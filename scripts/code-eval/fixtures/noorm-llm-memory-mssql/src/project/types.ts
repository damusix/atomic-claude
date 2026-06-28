/**
 * Row + proc contracts for the Project domain.
 *
 * Project belongs to an Agent (sentinel 0 = system). Project_Tag is the
 * binary join between projects and tags; it lives here because the join
 * carries no domain logic of its own.
 */
import type { Generated } from 'kysely';

/** Physical Project table row. project_id 0 is the protected sentinel. */
export interface ProjectRow {
    project_id: Generated<number>;
    agent_id: Generated<number>;
    name: string;
    filepath: Generated<string>;
    git_repo: Generated<string>;
    main_branch: Generated<string>;
    git_url: Generated<string>;
    created_at: Generated<Date>;
    updated_at: Generated<Date>;
}

/** Binary join: a Project tagged with a Tag. */
export interface ProjectTagRow {
    tag_id: number;
    project_id: number;
    created_at: Generated<Date>;
}

/** Stored procedure contracts owned by the Project domain. */
export interface ProjectProcs {

    'sp_Project_Create': [
        {
            name: string;
            filepath?: string;
            git_repo?: string;
            main_branch?: string;
            git_url?: string;
            agent_id?: number;
        },
        { project_id: number },
    ];

    'sp_Project_Update': [
        {
            project_id: number;
            name: string;
            filepath?: string;
            git_repo?: string;
            main_branch?: string;
            git_url?: string;
        },
        void,
    ];

    'sp_Project_Delete': [
        { project_id: number },
        void,
    ];

}

/**
 * Public surface for the Audit domain.
 *
 * Audit is read-only — there is no commands.ts and no schema.ts. The
 * StateTransition basetype row + the unified vw_StateTransition view
 * row are exposed alongside the AuditQueries class so callers can
 * project per-entity history, recent activity, and agent rollups.
 */
export { AuditQueries } from './queries';

export type {
    AgentActivityRow,
    AuditEntityType,
    RecentActivityRow,
} from './queries';

export type {
    StateTransitionRow,
    AuditViewRow,
} from './types';

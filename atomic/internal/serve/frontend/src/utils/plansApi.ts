// plansApi — typed shapes and fetchers for GET /api/plans[?member=] and
// GET /api/plans/members. Module-level `api` + `attempt()`, same non-React
// pattern as graphEngineAdapter's fetchGraphMembers — never a hand-rolled
// `fetch`. See docs/spec/serve-plans-page.md's payload contract.
import { attempt } from "@logosdx/utils";
import { api } from "./api";
import type { PageResponse } from "../pages/Page/types";

export interface PlanCheckout {
  id: string;
  branch: string;
  path: string;
  outsideRoot: boolean;
  isMain: boolean;
  fileMtime: string;
  created?: string;
}

export interface PlanDocVersion {
  sha: string;
  label: string;
  isMain: boolean;
  mtime: string;
  checkouts: PlanCheckout[];
}

export interface PlanDoc {
  path: string;
  versions: PlanDocVersion[];
}

export interface BundleFile {
  relpath: string;
  kind: "markdown" | "html" | "file";
}

export interface PlanBundle {
  worktreeId: string;
  purposes: string[];
  status: string;
  files: BundleFile[];
}

export interface PlanRow {
  slug: string;
  title: string;
  description: string;
  docs: PlanDoc[];
  bundles: PlanBundle[];
  dotCount: number;
  dotMerged: boolean;
  updatedAt?: string;
}

export interface PlansMember {
  key: string;
  prefix: string;
}

interface PlansMembersResponse {
  members: PlansMember[];
}

export async function fetchPlans(member?: string): Promise<PlanRow[]> {
  const path = member ? `/plans?member=${encodeURIComponent(member)}` : "/plans";
  const [res, err] = await attempt(() => api.get<PlanRow[]>(path));
  if (err || !res?.ok || !res.data) return [];
  return res.data;
}

export async function fetchPlanMembers(): Promise<PlansMember[]> {
  const [res, err] = await attempt(() => api.get<PlansMembersResponse>("/plans/members"));
  if (err || !res?.ok || !res.data) return [];
  return res.data.members ?? [];
}

/**
 * Fetches the rendered form of one doc or bundle file. Raw bytes (the html
 * and file bundle kinds) are never fetched through here — the viewer points
 * an iframe src or a download href at the raw URL so the bytes stay out of
 * the React tree.
 */
export async function fetchPlanPage(worktreeId: string, path: string): Promise<PageResponse | null> {
  const params = new URLSearchParams({ worktree: worktreeId, path });
  const [res, err] = await attempt(() => api.get<PageResponse>(`/plans/page?${params.toString()}`));
  if (err || !res?.ok || !res.data) return null;
  return res.data;
}

/**
 * A bundle file's relpath is worktree-relative — the page handler resolves it
 * under the checkout root — so the part the reader recognises is whatever
 * follows the bundle directory. Returns the input unchanged when it does not
 * sit under a scratchpad bundle.
 */
export function bundleLocalPath(relpath: string): string {
  const marker = "/.scratchpad/";
  const i = relpath.indexOf(marker);
  if (i < 0) return relpath;
  const rest = relpath.slice(i + marker.length);
  const slash = rest.indexOf("/");
  return slash < 0 ? rest : rest.slice(slash + 1);
}

/**
 * Short day-month rendering of an API timestamp. Returns "" for anything that
 * is not a real date, including the zero time a value-less row would carry,
 * so a caller renders nothing rather than "1 Jan 0001".
 */
export function formatDate(iso: string | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 1970) return "";
  return d.toLocaleDateString(undefined, { day: "numeric", month: "short" });
}

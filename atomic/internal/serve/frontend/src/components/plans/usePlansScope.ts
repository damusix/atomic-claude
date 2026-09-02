// usePlansScope — the one place Plans code reads ?at=/the /plans/:slug route
// and the one place it writes them. Every consumer used to re-derive this
// from its own useSearchParams/useLocation call and assemble its own /plans
// URL; that let five sites drift out of sync. This hook is the single
// source, so a consumer either reads its fields or calls one of its writers —
// never react-router directly. Which member is selected lives in
// utils/memberStore, not here — it is not a URL concern.
import { useLocation, useNavigate, useSearchParams } from "react-router";

export interface PlansScope {
  at: string | undefined;
  /** The opened slug, from the /plans/:slug route — undefined on bare /plans. */
  slug: string | undefined;
  /** The /plans/:slug/* remainder, when a file is open within the slug. */
  relpath: string | undefined;
  /** True on /plans and /plans/…; false on /plans-anything-else and everywhere else. */
  isPlansRoute: boolean;
  openSlug(slug: string): void;
  openFile(relpath: string, opts?: { replace?: boolean; at?: string }): void;
  setAt(branch: string, opts?: { replace?: boolean }): void;
  plansHref(): string;
  slugHref(slug: string): string;
}

function isPlansPath(pathname: string): boolean {
  return pathname === "/plans" || pathname.startsWith("/plans/");
}

function parsePlansPath(pathname: string): { slug: string | undefined; relpath: string | undefined } {
  if (!isPlansPath(pathname)) return { slug: undefined, relpath: undefined };
  const rest = pathname.slice("/plans".length).replace(/^\//, "");
  if (!rest) return { slug: undefined, relpath: undefined };
  const slash = rest.indexOf("/");
  if (slash === -1) return { slug: rest, relpath: undefined };
  return { slug: rest.slice(0, slash), relpath: rest.slice(slash + 1) || undefined };
}

export function usePlansScope(): PlansScope {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const location = useLocation();

  const at = searchParams.get("at") ?? undefined;
  const isPlansRoute = isPlansPath(location.pathname);
  const { slug, relpath } = parsePlansPath(location.pathname);

  function plansHref(): string {
    return "/plans";
  }

  // Keeps ?at= where openSlug drops it: this is the breadcrumb back to the
  // slug the reader is already in, so the version they are reading stays
  // selected. openSlug targets a different slug, whose versions are its own.
  function slugHref(targetSlug: string): string {
    return `/plans/${targetSlug}${at ? `?at=${encodeURIComponent(at)}` : ""}`;
  }

  function openSlug(targetSlug: string): void {
    navigate(`/plans/${targetSlug}`);
  }

  function openFile(targetRelpath: string, opts?: { replace?: boolean; at?: string }): void {
    if (!slug) return;
    let search = location.search;
    if (opts?.at) {
      const next = new URLSearchParams(searchParams);
      next.set("at", opts.at);
      search = `?${next.toString()}`;
    }
    navigate(`/plans/${slug}/${targetRelpath}${search}`, { replace: opts?.replace });
  }

  // Not setSearchParams — react-router's implementation navigates via
  // `"?" + params`, which carries no hash, so a heading anchor open at the
  // time of the write would be silently dropped from the URL.
  function setAt(branch: string, opts?: { replace?: boolean }): void {
    const next = new URLSearchParams(searchParams);
    next.set("at", branch);
    navigate(
      { pathname: location.pathname, search: `?${next.toString()}`, hash: location.hash },
      { replace: opts?.replace },
    );
  }

  return { at, slug, relpath, isPlansRoute, openSlug, openFile, setAt, plansHref, slugHref };
}

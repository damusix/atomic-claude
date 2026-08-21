// usePlansScope — the one place Plans code reads ?member=/?at=/the /plans/:slug
// route and the one place it writes them. Every consumer used to re-derive
// this from its own useSearchParams/useLocation call and assemble its own
// /plans URL; that let five sites drift out of sync. This hook is the single
// source, so a consumer either reads its fields or calls one of its writers —
// never react-router directly.
import { useLocation, useNavigate, useSearchParams } from "react-router";

export interface PlansScope {
  member: string | undefined;
  at: string | undefined;
  /** The opened slug, from the /plans/:slug route — undefined on bare /plans. */
  slug: string | undefined;
  /** The /plans/:slug/* remainder, when a file is open within the slug. */
  relpath: string | undefined;
  /** True on /plans and /plans/…; false on /plans-anything-else and everywhere else. */
  isPlansRoute: boolean;
  openSlug(slug: string): void;
  openFile(relpath: string, opts?: { replace?: boolean }): void;
  setAt(branch: string, opts?: { replace?: boolean }): void;
  setMember(key: string): void;
  /** "?member=…" (or "") for a link scoped to the current member. */
  scopedSearch(): string;
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

  const member = searchParams.get("member") ?? undefined;
  const at = searchParams.get("at") ?? undefined;
  const isPlansRoute = isPlansPath(location.pathname);
  const { slug, relpath } = parsePlansPath(location.pathname);

  function scopedSearch(): string {
    return member ? `?${new URLSearchParams({ member }).toString()}` : "";
  }

  function plansHref(): string {
    return `/plans${scopedSearch()}`;
  }

  // Keeps ?at= where openSlug drops it: this is the breadcrumb back to the
  // slug the reader is already in, so the version they are reading stays
  // selected. openSlug targets a different slug, whose versions are its own.
  function slugHref(targetSlug: string): string {
    const params = new URLSearchParams();
    if (member) params.set("member", member);
    if (at) params.set("at", at);
    const qs = params.toString();
    return `/plans/${targetSlug}${qs ? `?${qs}` : ""}`;
  }

  function openSlug(targetSlug: string): void {
    navigate(`/plans/${targetSlug}${scopedSearch()}`);
  }

  function openFile(targetRelpath: string, opts?: { replace?: boolean }): void {
    if (!slug) return;
    navigate(`/plans/${slug}/${targetRelpath}${location.search}`, { replace: opts?.replace });
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

  function setMember(key: string): void {
    const next = new URLSearchParams(searchParams);
    if (key) next.set("member", key);
    else next.delete("member");
    next.delete("at");
    const qs = next.toString();
    navigate({ pathname: location.pathname, search: qs ? `?${qs}` : "", hash: location.hash });
  }

  return { member, at, slug, relpath, isPlansRoute, openSlug, openFile, setAt, setMember, scopedSearch, plansHref, slugHref };
}

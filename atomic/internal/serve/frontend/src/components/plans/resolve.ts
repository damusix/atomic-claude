// resolve — pure functions shared by SlugView (owns the fetch + the sticky
// `?at=` write) and PlansRail (read-only render of the same resolution) so
// the two can never compute a different answer for "what checkout is on
// screen". See docs/design/serve-plans-page.md "A version selection follows
// the reader, and yields rather than blocks".
import type { BundleFile, PlanBundle, PlanCheckout, PlanDoc, PlanDocVersion, PlanRow } from "../../utils/plansApi";

export interface DocResolution {
  doc: PlanDoc;
  version: PlanDocVersion;
  checkout: PlanCheckout;
  /** True when the selection (`at`) named this doc's own checkout — false
      means this resolution is a yield to the doc's default. */
  held: boolean;
}

export interface BundleResolution {
  bundle: PlanBundle;
  file: BundleFile;
  checkout?: PlanCheckout;
}

export function findDoc(row: PlanRow | null, relpath: string): PlanDoc | undefined {
  if (!relpath) return undefined;
  return row?.docs?.find((d) => d.path === relpath);
}

function mergedOrNewest(versions: PlanDocVersion[]): PlanDocVersion | undefined {
  return versions.find((v) => v.isMain) ?? [...versions].sort((a, b) => Date.parse(b.mtime) - Date.parse(a.mtime))[0];
}

export function resolveDocVersion(doc: PlanDoc, at: string | undefined): DocResolution | null {
  if (!doc.versions.length) return null;
  const held = at ? doc.versions.find((v) => v.checkouts.some((c) => c.branch === at)) : undefined;
  const version = held ?? mergedOrNewest(doc.versions);
  if (!version) return null;
  const checkout =
    (held ? version.checkouts.find((c) => c.branch === at) : undefined) ??
    version.checkouts.find((c) => c.branch === version.label) ??
    version.checkouts[0];
  if (!checkout) return null;
  return { doc, version, checkout, held: Boolean(held) };
}

export function findCheckoutById(row: PlanRow | null, id: string): PlanCheckout | undefined {
  for (const doc of row?.docs ?? []) {
    for (const v of doc.versions) {
      const c = v.checkouts.find((c) => c.id === id);
      if (c) return c;
    }
  }
  return undefined;
}

export function resolveBundleFile(row: PlanRow | null, relpath: string): BundleResolution | null {
  if (!relpath) return null;
  for (const bundle of row?.bundles ?? []) {
    const file = bundle.files.find((f) => f.relpath === relpath);
    if (file) return { bundle, file, checkout: findCheckoutById(row, bundle.worktreeId) };
  }
  return null;
}

// utils/events — the cross-cutting event bus. A typed ObserverEngine
// (@logosdx/observer) instance shared across the app via React context
// (createObserverContext): `useTheme` (CP5) emits "theme.changed" so the
// retheme cascade (mermaid/cosmos/rail-Cytoscape, CP11) can subscribe;
// `useLiveReload` (CP11) emits "realm.changed". Never a second event bus.
import { ObserverEngine } from "@logosdx/observer";
import { createObserverContext } from "@logosdx/react";

export type Theme = "light" | "dark";

export interface AppEvents {
  "theme.changed": { theme: Theme };
  "realm.changed": { fp: string; changed?: string[] };
  // Emitted by pages/Page once /api/page resolves — carries the server-resolved
  // relpath (directory URLs resolve to their index file) so components/rail
  // (mounted in the Shell aside, outside Page's subtree) knows which page to
  // fetch /api/rail for. relpath is null for directory listings and 404s,
  // which have no rail (rail requires graph membership).
  "page.resolved": { relpath: string | null };
  // Emitted by pages/Page after the server HTML is injected and its headings
  // are readable. components/rail builds the on-page contents from this — the
  // rail is mounted outside Page's subtree and never sees the body itself.
  "page.headings": { headings: PageHeading[] };
  // Emitted by components/schema once its tables are grouped. The schema route
  // has no page and therefore no /api/rail payload, so its navigation has to
  // reach the rail the same way headings do: through the bus, from inside the
  // Outlet subtree the rail is mounted outside of.
  "schema.index": SchemaIndex;
}

/** One entry in a page's on-page contents. */
export interface PageHeading {
  id: string;
  text: string;
  /** Heading level, 1-4. Deeper levels are not surfaced. */
  level: number;
}

/** A directory or file in the schema index, as the rail renders it. */
export interface SchemaIndexEntry {
  /** Anchor id of the section or group it scrolls to. */
  id: string;
  title: string;
  count: number;
  children?: SchemaIndexEntry[];
}

export interface SchemaIndex {
  /** Which member the index describes — the rail keys its state on this so a
      member switch does not leave the previous member's tree expanded. */
  member: string;
  sections: SchemaIndexEntry[];
}

export const events = new ObserverEngine<AppEvents>();

export const [EventsProvider, useEvents] = createObserverContext(events);

// The headings latch. components/rail mounts only after /api/rail resolves,
// which is strictly after pages/Page has already injected its HTML and
// emitted — so a plain subscription always misses the event that matters and
// the contents list renders empty. Emitters go through here so a late
// subscriber can read the current value on mount.
let latestHeadings: PageHeading[] = [];

export function emitPageHeadings(headings: PageHeading[]): void {
  latestHeadings = headings;
  events.emit("page.headings", { headings });
}

export function getPageHeadings(): PageHeading[] {
  return latestHeadings;
}

// The schema index latches for the same reason: the rail may mount before or
// after the schema data resolves, and only one of those orders would work
// with a plain subscription.
let latestSchemaIndex: SchemaIndex = { member: "", sections: [] };

export function emitSchemaIndex(index: SchemaIndex): void {
  latestSchemaIndex = index;
  events.emit("schema.index", index);
}

export function getSchemaIndex(): SchemaIndex {
  return latestSchemaIndex;
}

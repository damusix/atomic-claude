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
}

export const events = new ObserverEngine<AppEvents>();

export const [EventsProvider, useEvents] = createObserverContext(events);

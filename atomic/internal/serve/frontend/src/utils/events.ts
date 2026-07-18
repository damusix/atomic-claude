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
}

export const events = new ObserverEngine<AppEvents>();

export const [EventsProvider, useEvents] = createObserverContext(events);

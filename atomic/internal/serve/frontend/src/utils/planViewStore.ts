// planViewStore — the checkout of the file SlugView currently has on
// screen, module-level so TopBar can read it without SlugView passing props
// through the router-rendered tree. Same useSyncExternalStore pattern as
// memberStore. See docs/spec/serve-realm-ux.md "Provenance".
import { useSyncExternalStore } from "react";

export interface OnScreenCheckout {
  branch: string;
  path: string;
  outsideRoot: boolean;
}

type Listener = () => void;

let state: OnScreenCheckout | null = null;
const listeners = new Set<Listener>();

function setState(next: OnScreenCheckout | null): void {
  state = next;
  for (const listener of listeners) listener();
}

export function subscribe(listener: Listener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function getOnScreen(): OnScreenCheckout | null {
  return state;
}

export function setOnScreen(checkout: OnScreenCheckout): void {
  setState(checkout);
}

export function clearOnScreen(): void {
  setState(null);
}

export function useOnScreenCheckout(): OnScreenCheckout | null {
  return useSyncExternalStore(subscribe, getOnScreen);
}

export function __resetForTest(): void {
  state = null;
  listeners.clear();
}

// memberStore — the reader's picked member (which repo, in realm scope),
// module-level and cookie-backed so it survives a reload without living in
// the URL. useSyncExternalStore, same pattern as components/code-modal/store.ts.
// One GET /nav per page load resolves the scope identity (`<scope>:<name>`);
// until that resolves, `ready` is false and callers hold their member-scoped
// fetch. See docs/spec/serve-realm-ux.md "Member store".
import { useEffect, useSyncExternalStore } from "react";
import { attempt } from "@logosdx/utils";
import { api } from "./api";
import type { NavResponse } from "../components/nav/types";

const COOKIE_NAME = "atomic-member";

interface Identity {
  scope: "realm" | "repo";
  name: string;
}

interface MemberState {
  identity: Identity | null;
  ready: boolean;
  member: string;
}

type Listener = () => void;

let state: MemberState = { identity: null, ready: false, member: "" };
const listeners = new Set<Listener>();
let inFlight: Promise<void> | null = null;

function setState(next: MemberState): void {
  state = next;
  for (const listener of listeners) listener();
}

export function subscribe(listener: Listener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function getState(): MemberState {
  return state;
}

// Every access is wrapped: a missing `document` (SSR, non-browser test) or a
// malformed cookie value must never throw, only yield the empty map.
function readCookieMap(): Record<string, string> {
  try {
    if (typeof document === "undefined") return {};
    const entry = document.cookie.split("; ").find((c) => c.startsWith(`${COOKIE_NAME}=`));
    if (!entry) return {};
    const parsed = JSON.parse(decodeURIComponent(entry.slice(COOKIE_NAME.length + 1)));
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

function writeCookieMap(map: Record<string, string>): void {
  try {
    if (typeof document === "undefined") return;
    document.cookie = `${COOKIE_NAME}=${encodeURIComponent(JSON.stringify(map))}; path=/; max-age=31536000; SameSite=Lax`;
  } catch {
    // Cookie writes can throw under third-party-cookie or storage-access
    // restrictions — the pick simply doesn't persist this time.
  }
}

export function ensureIdentity(): Promise<void> {
  if (inFlight) return inFlight;
  inFlight = attempt(() => api.get<NavResponse>("/nav")).then(([res, err]) => {
    if (err || !res?.ok || !res.data) {
      setState({ identity: null, ready: true, member: "" });
      return;
    }
    const identity: Identity = { scope: res.data.scope, name: res.data.name };
    const member = readCookieMap()[`${identity.scope}:${identity.name}`] ?? "";
    setState({ identity, ready: true, member });
  });
  return inFlight;
}

export function setMember(key: string): void {
  const { identity } = state;
  setState({ ...state, member: key });
  if (!identity) return;
  const map = readCookieMap();
  map[`${identity.scope}:${identity.name}`] = key;
  writeCookieMap(map);
}

export interface CurrentMember {
  member: string;
  ready: boolean;
  scope: "realm" | "repo" | undefined;
  realmName: string;
  setMember: (key: string) => void;
}

export function useCurrentMember(): CurrentMember {
  const snapshot = useSyncExternalStore(subscribe, getState);
  useEffect(() => {
    void ensureIdentity();
  }, []);
  return {
    member: snapshot.member,
    ready: snapshot.ready,
    scope: snapshot.identity?.scope,
    realmName: snapshot.identity?.name ?? "",
    setMember,
  };
}

export function memberLabel(prefix: string, realmName: string): string {
  return prefix === "" ? realmName : prefix;
}

// Test-only: wait for a fire-and-forget identity probe to finish. `useCurrentMember`
// starts `ensureIdentity()` without awaiting it, so a test that asserts something
// not gated on `ready` can end with the request still running. Once the suite's
// teardown deletes `globalThis.fetch`, that request throws and the FetchEngine
// retries it 150ms later — by which point the NEXT test file has installed its own
// fetch spy, which then counts a call it never made. Draining here keeps a probe
// from outliving the file that started it.
export async function __settleForTest(): Promise<void> {
  const pending = inFlight;
  if (!pending) return;
  await pending.catch(() => {});
}

export function __resetForTest(): void {
  state = { identity: null, ready: false, member: "" };
  listeners.clear();
  inFlight = null;
}

// store — the code modal's open/back-stack state, subscribable via
// useSyncExternalStore (CodeModal.tsx) and mutated both from React (Page
// link interception, Rail codeFile links, the search palette's code result)
// and from the carried vanilla code-graph.js, which has no access to React
// state and instead calls window.AtomicCodeExplorer.openNode(id, member,
// meta) — the same bridge pattern utils/graphUI's window.AtomicGraphUI
// already established for the docs preview-card/page-modal hooks.
import type { IntelTarget } from "./types";

export interface StackEntry {
  // filePath is the realm-relative /file/ path for the source pane, or null
  // when the current entry has no known location (e.g. a node with no file).
  filePath: string | null;
  line: number | null;
  title: string;
  intel: IntelTarget;
}

interface CodeModalState {
  open: boolean;
  stack: StackEntry[];
}

type Listener = () => void;

let state: CodeModalState = { open: false, stack: [] };
const listeners = new Set<Listener>();

function setState(next: CodeModalState): void {
  state = next;
  for (const listener of listeners) listener();
}

export function subscribe(listener: Listener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function getState(): CodeModalState {
  return state;
}

// openFile opens the modal on a source file (page-body link, rail codeFile
// edge), seeding the back-stack with the file's defines view as the root —
// mirrors templates/layout.html's openModal(filePath, anchor).
export function openFile(path: string, line: number | null = null): void {
  setState({
    open: true,
    stack: [{ filePath: path, line, title: path, intel: { kind: "file", path } }],
  });
}

// openNode opens the existing code-explorer node view for one symbol —
// the code-graph click hook's call signature (code-graph.js's onClick),
// member-aware. meta rides along from the graph engine's own hover-meta
// resolver (title/file/line) so the source pane can open without waiting on
// the node fetch — mirrors templates/layout.html's openCodeNode.
export function openNode(
  id: string,
  member: string,
  meta?: { title?: string; file?: string; line?: number } | null,
): void {
  const filePath = meta?.file ? (member ? `${member}/${meta.file}` : meta.file) : null;
  setState({
    open: true,
    stack: [
      {
        filePath,
        line: meta?.line ?? null,
        title: meta?.title || id,
        intel: { kind: "node", id, member },
      },
    ],
  });
}

// pushIntel records a forward drill-down (defines → node → callers/callees/
// impact → node → …) onto the back-stack.
export function pushIntel(entry: StackEntry): void {
  setState({ open: state.open, stack: [...state.stack, entry] });
}

// popIntel pops one level off the back-stack. A no-op at the root entry —
// the persistent Back button is hidden there (mirrors the carried
// intelGoBack's stack.length <= 1 guard).
export function popIntel(): void {
  if (state.stack.length <= 1) return;
  setState({ open: state.open, stack: state.stack.slice(0, -1) });
}

export function closeModal(): void {
  setState({ open: false, stack: [] });
}

export const AtomicCodeExplorer = { openNode };

export function installCodeExplorerGlobal(target: Window = window): void {
  target.AtomicCodeExplorer = AtomicCodeExplorer;
}

declare global {
  interface Window {
    AtomicCodeExplorer: typeof AtomicCodeExplorer;
  }
}

// Test-only reset — clears module-level state between test cases.
export function __resetForTest(): void {
  state = { open: false, stack: [] };
  listeners.clear();
}

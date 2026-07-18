// useTheme — light/dark toggle, persisted to localStorage under the same
// key index.html's before-paint inline script reads ("atomic-theme"), with
// an OS-preference fallback when nothing is persisted. Emits "theme.changed"
// on the shared observer bus (utils/events), and — since TopBar mounts this
// hook exactly once for the whole app — also owns the retheme cascade
// subscription itself: every "theme.changed" (including the one this same
// hook instance just emitted) re-invokes the three typeColors-derived
// consumers named in the spec's retheme-cascade Flow. Each re-reads the
// now-flipped CSS vars and repaints; none re-fetches data.
import { useCallback, useEffect, useState } from "react";
import { attemptSync } from "@logosdx/utils";
import { rethemeRailGraph } from "../components/rail/railCytoscapeStyle";
import { events, type Theme } from "../utils/events";
import { rethemeGraph } from "../utils/graphEngineAdapter";
import { rethemeMermaid } from "../utils/mermaid";

const STORAGE_KEY = "atomic-theme";

function readPersistedTheme(): Theme | null {
  const [stored] = attemptSync(() => window.localStorage.getItem(STORAGE_KEY));
  return stored === "light" || stored === "dark" ? stored : null;
}

function readOSPreference(): Theme {
  const [prefersDark] = attemptSync(
    () => window.matchMedia("(prefers-color-scheme: dark)").matches,
  );
  return prefersDark ? "dark" : "light";
}

function resolveInitialTheme(): Theme {
  return readPersistedTheme() ?? readOSPreference();
}

function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme;
}

export function useTheme() {
  const [theme, setTheme] = useState<Theme>(resolveInitialTheme);

  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  useEffect(() => {
    return events.on("theme.changed", () => {
      rethemeRailGraph();
      rethemeGraph();
      void rethemeMermaid();
    });
  }, []);

  const toggle = useCallback(() => {
    setTheme((current) => {
      const next: Theme = current === "dark" ? "light" : "dark";
      attemptSync(() => window.localStorage.setItem(STORAGE_KEY, next));
      events.emit("theme.changed", { theme: next });
      return next;
    });
  }, []);

  return { theme, toggle };
}

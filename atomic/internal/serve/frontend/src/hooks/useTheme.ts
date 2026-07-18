// useTheme — light/dark toggle, persisted to localStorage under the same
// key index.html's before-paint inline script reads ("atomic-theme"), with
// an OS-preference fallback when nothing is persisted. Emits "theme.changed"
// on the shared observer bus (utils/events) so the retheme cascade
// (mermaid/cosmos/rail-Cytoscape consumers, wired in CP11) can react without
// this hook knowing who's listening.
import { useCallback, useEffect, useState } from "react";
import { attemptSync } from "@logosdx/utils";
import { events, type Theme } from "../utils/events";

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

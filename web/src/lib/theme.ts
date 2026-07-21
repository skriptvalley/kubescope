// Theme store (ADR-0009): light / system / dark, persisted to localStorage and,
// for "system", following the OS via matchMedia. Toggling stamps `.dark` on
// <html>. A tiny inline script in index.html applies the stored theme before
// first paint (no flash); this module is the runtime source of truth the header
// toggle reads and writes. Shaped as an external store for useSyncExternalStore.

export type Theme = "light" | "system" | "dark";

const STORAGE_KEY = "kubescope-theme";
const THEMES: Theme[] = ["light", "system", "dark"];

function isTheme(v: string | null): v is Theme {
  return v === "light" || v === "system" || v === "dark";
}

function read(): Theme {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (isTheme(stored)) return stored;
  } catch {
    // localStorage unavailable (private mode / SSR) — fall back to system.
  }
  return "system";
}

const media = typeof window !== "undefined" && window.matchMedia
  ? window.matchMedia("(prefers-color-scheme: dark)")
  : undefined;

/** Whether the given theme resolves to dark right now (system consults the OS). */
export function resolvesDark(theme: Theme): boolean {
  return theme === "dark" || (theme === "system" && !!media?.matches);
}

/** Applies the theme to <html> by toggling `.dark`. */
function apply(theme: Theme): void {
  if (typeof document === "undefined") return;
  document.documentElement.classList.toggle("dark", resolvesDark(theme));
}

let current = read();
const listeners = new Set<() => void>();

function emit(): void {
  for (const l of listeners) l();
}

// Re-apply when the OS scheme changes while following "system".
media?.addEventListener("change", () => {
  if (current === "system") {
    apply(current);
    emit();
  }
});

export const themeStore = {
  themes: THEMES,
  get(): Theme {
    return current;
  },
  set(theme: Theme): void {
    current = theme;
    try {
      localStorage.setItem(STORAGE_KEY, theme);
    } catch {
      // Persistence best-effort; the applied class still takes effect.
    }
    apply(theme);
    emit();
  },
  subscribe(listener: () => void): () => void {
    listeners.add(listener);
    return () => listeners.delete(listener);
  },
};

// Ensure the runtime class matches the store on load (the inline head script
// runs first; this reconciles if it was absent, e.g. in tests).
apply(current);

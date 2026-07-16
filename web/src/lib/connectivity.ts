// A tiny module-level connectivity store (FB-6 Story D, no new deps). It holds
// two booleans shared across the app without prop-drilling or a context:
//   - activeUnreachable: the active context is currently unreachable, so pollers
//     back off and the unreachable banner shows.
//   - everConnected: we have connected at least once, which decides whether an
//     unreachable active context shows the full-page starter (never connected)
//     or the in-app banner (was working, now degraded).
// The subscribe/getSnapshot shape is compatible with React's useSyncExternalStore.

type Listener = () => void;

let activeUnreachable = false;
let everConnected = false;
const listeners = new Set<Listener>();

function emit(): void {
  for (const listener of listeners) listener();
}

export const connectivity = {
  setActiveUnreachable(value: boolean): void {
    if (activeUnreachable === value) return;
    activeUnreachable = value;
    emit();
  },
  isActiveUnreachable(): boolean {
    return activeUnreachable;
  },
  markEverConnected(): void {
    if (everConnected) return;
    everConnected = true;
    emit();
  },
  hasEverConnected(): boolean {
    return everConnected;
  },
  subscribe(fn: Listener): () => void {
    listeners.add(fn);
    return () => {
      listeners.delete(fn);
    };
  },
  /** Clears both flags and listeners. Test-only — the store is a singleton, so
   *  suites reset it between cases to avoid cross-test bleed. */
  resetForTests(): void {
    activeUnreachable = false;
    everConnected = false;
    listeners.clear();
  },
};

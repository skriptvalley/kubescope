import "@testing-library/jest-dom/vitest";

import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// jsdom has no matchMedia. Provide a controllable stub whose `matches` reads a
// mutable global, so theme tests can simulate the OS preferring dark by setting
// `(globalThis as { __prefersDark?: boolean }).__prefersDark = true`.
if (!window.matchMedia) {
  window.matchMedia = ((query: string) =>
    ({
      media: query,
      get matches() {
        return Boolean((globalThis as { __prefersDark?: boolean }).__prefersDark);
      },
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    })) as unknown as typeof window.matchMedia;
}

// vitest runs without global injection, so React Testing Library's automatic
// afterEach cleanup isn't registered — do it here to unmount between tests.
afterEach(() => {
  cleanup();
});

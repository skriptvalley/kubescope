import "@testing-library/jest-dom/vitest";

import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// vitest runs without global injection, so React Testing Library's automatic
// afterEach cleanup isn't registered — do it here to unmount between tests.
afterEach(() => {
  cleanup();
});

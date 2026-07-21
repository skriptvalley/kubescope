import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { resolvesDark, themeStore } from "@/lib/theme";

import { ThemeToggle } from "./theme-toggle";

beforeEach(() => {
  localStorage.clear();
  (globalThis as { __prefersDark?: boolean }).__prefersDark = false;
  themeStore.set("light");
});
afterEach(() => {
  document.documentElement.classList.remove("dark");
});

describe("ThemeToggle", () => {
  it("renders three theme options as a radiogroup", () => {
    render(<ThemeToggle />);
    expect(screen.getByRole("radiogroup", { name: /color theme/i })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "Light" })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "System" })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "Dark" })).toBeInTheDocument();
  });

  it("applies dark and persists it on selecting Dark", () => {
    render(<ThemeToggle />);
    fireEvent.click(screen.getByRole("radio", { name: "Dark" }));
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(localStorage.getItem("kubescope-theme")).toBe("dark");
    expect(screen.getByRole("radio", { name: "Dark" })).toHaveAttribute("aria-checked", "true");
  });

  it("removes dark on selecting Light", () => {
    themeStore.set("dark");
    render(<ThemeToggle />);
    fireEvent.click(screen.getByRole("radio", { name: "Light" }));
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    expect(localStorage.getItem("kubescope-theme")).toBe("light");
  });

  it("system follows the OS preference via matchMedia", () => {
    (globalThis as { __prefersDark?: boolean }).__prefersDark = true;
    render(<ThemeToggle />);
    fireEvent.click(screen.getByRole("radio", { name: "System" }));
    expect(localStorage.getItem("kubescope-theme")).toBe("system");
    expect(document.documentElement.classList.contains("dark")).toBe(true);

    (globalThis as { __prefersDark?: boolean }).__prefersDark = false;
    themeStore.set("system"); // re-resolve against the new OS preference
    expect(document.documentElement.classList.contains("dark")).toBe(false);
  });
});

describe("resolvesDark", () => {
  it("resolves explicit themes independent of the OS", () => {
    (globalThis as { __prefersDark?: boolean }).__prefersDark = true;
    expect(resolvesDark("dark")).toBe(true);
    expect(resolvesDark("light")).toBe(false);
    expect(resolvesDark("system")).toBe(true);
    (globalThis as { __prefersDark?: boolean }).__prefersDark = false;
    expect(resolvesDark("system")).toBe(false);
  });
});

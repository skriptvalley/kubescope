import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { GlobalSearch } from "@/components/global-search";
import type { SearchResponse } from "@/lib/api";

const searchMock = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: { ...original.api, search: searchMock } };
});

const RESPONSE: SearchResponse = {
  query: "web",
  results: [
    {
      group: "",
      version: "v1",
      resource: "services",
      kind: "Service",
      namespace: "default",
      name: "web",
      namespaced: true,
    },
    {
      group: "apps",
      version: "v1",
      resource: "deployments",
      kind: "Deployment",
      namespace: "default",
      name: "web-deploy",
      namespaced: true,
    },
  ],
  truncated: false,
};

function LocationDisplay() {
  const loc = useLocation();
  return <div data-testid="location">{loc.pathname}</div>;
}

function renderSearch() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/overview"]}>
        <GlobalSearch />
        <LocationDisplay />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("GlobalSearch", () => {
  beforeEach(() => {
    searchMock.mockReset();
    searchMock.mockResolvedValue(RESPONSE);
  });

  it("focuses the input when '/' is pressed", () => {
    renderSearch();
    const input = screen.getByRole("combobox", { name: "Search resources" });
    expect(input).not.toHaveFocus();
    fireEvent.keyDown(document.body, { key: "/" });
    expect(input).toHaveFocus();
  });

  it("searches on input and navigates to the arrow-selected result on Enter", async () => {
    renderSearch();
    const input = screen.getByRole("combobox", { name: "Search resources" });

    fireEvent.change(input, { target: { value: "web" } });
    await screen.findByText("web-deploy");
    expect(searchMock).toHaveBeenCalledWith("web", 20);

    // ArrowDown moves selection from the first result to the second, Enter opens it.
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(screen.getByTestId("location")).toHaveTextContent(
      "/resources/apps/v1/deployments/default/web-deploy",
    );
  });

  it("navigates to the first result by default (core token in the route)", async () => {
    renderSearch();
    const input = screen.getByRole("combobox", { name: "Search resources" });
    fireEvent.change(input, { target: { value: "web" } });
    await screen.findByText("web");

    fireEvent.keyDown(input, { key: "Enter" });
    expect(screen.getByTestId("location")).toHaveTextContent(
      "/resources/core/v1/services/default/web",
    );
  });

  it("closes the results on Escape", async () => {
    renderSearch();
    const input = screen.getByRole("combobox", { name: "Search resources" });
    fireEvent.change(input, { target: { value: "web" } });
    await screen.findByRole("listbox");

    fireEvent.keyDown(input, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
  });

  it("does not search for a single character", async () => {
    renderSearch();
    const input = screen.getByRole("combobox", { name: "Search resources" });
    fireEvent.change(input, { target: { value: "w" } });
    // Give the debounce time to (not) fire.
    await new Promise((r) => setTimeout(r, 300));
    expect(searchMock).not.toHaveBeenCalled();
  });
});

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type ResourceRef } from "@/lib/api";

import { YamlTab } from "./yaml-tab";

// The CodeMirror editor is replaced with a plain textarea so the tab's
// apply/confirm/conflict/validation logic is tested without mounting CodeMirror
// (which needs DOM APIs jsdom does not fully provide). Its contract — value in,
// onChange out — is identical.
vi.mock("./yaml-editor", () => ({
  YamlEditor: ({ value, onChange }: { value: string; onChange: (v: string) => void }) => (
    <textarea data-testid="yaml-editor" value={value} onChange={(e) => onChange(e.target.value)} />
  ),
}));

const yamlMock = vi.hoisted(() => vi.fn());
const applyMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...original,
    api: { resources: { yaml: yamlMock, apply: applyMock } },
  };
});

const ref: ResourceRef = { group: "apps", version: "v1", resource: "deployments", namespace: "default", name: "web" };

function renderTab(readOnly = false) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <YamlTab refx={ref} kind="Deployment" readOnly={readOnly} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  yamlMock.mockReset();
  applyMock.mockReset();
  yamlMock.mockResolvedValue("apiVersion: apps/v1\nkind: Deployment\n");
});

describe("YamlTab", () => {
  it("hides the Edit affordance in read-only mode", async () => {
    renderTab(true);
    await screen.findByTestId("yaml-view");
    expect(screen.queryByRole("button", { name: /edit/i })).not.toBeInTheDocument();
  });

  it("applies an edit through the confirmation dialog", async () => {
    applyMock.mockResolvedValue({});
    renderTab();

    fireEvent.click(await screen.findByRole("button", { name: /edit/i }));
    fireEvent.change(screen.getByTestId("yaml-editor"), {
      target: { value: "apiVersion: apps/v1\nkind: Deployment\n# edited\n" },
    });
    fireEvent.click(screen.getByRole("button", { name: /save changes/i }));

    // Confirm dialog appears; applying calls the API with the edited draft.
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() =>
      expect(applyMock).toHaveBeenCalledWith(ref, "apiVersion: apps/v1\nkind: Deployment\n# edited\n"),
    );
    // On success we drop back to the read-only view.
    await screen.findByTestId("yaml-view");
  });

  it("surfaces a resourceVersion conflict with a reload path", async () => {
    applyMock.mockRejectedValue(new ApiError("stale", "conflict", 409));
    renderTab();

    fireEvent.click(await screen.findByRole("button", { name: /edit/i }));
    fireEvent.change(screen.getByTestId("yaml-editor"), { target: { value: "changed\n" } });
    fireEvent.click(screen.getByRole("button", { name: /save changes/i }));
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    const conflict = await screen.findByTestId("yaml-conflict");
    expect(conflict).toHaveTextContent(/changed in the cluster/i);
    expect(screen.getByRole("button", { name: /reload latest/i })).toBeInTheDocument();
  });

  it("shows server validation errors inline", async () => {
    applyMock.mockRejectedValue(new ApiError("spec.replicas invalid", "invalid", 422));
    renderTab();

    fireEvent.click(await screen.findByRole("button", { name: /edit/i }));
    fireEvent.change(screen.getByTestId("yaml-editor"), { target: { value: "bad\n" } });
    fireEvent.click(screen.getByRole("button", { name: /save changes/i }));
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    const err = await screen.findByTestId("yaml-validation-error");
    expect(err).toHaveTextContent(/spec.replicas invalid \(invalid\)/);
  });
});

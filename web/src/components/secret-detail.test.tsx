import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { KubeObject } from "@/lib/api";

import { SecretDetail } from "./secret-detail";

const revealMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...original,
    api: { secrets: { reveal: revealMock } },
  };
});

// The object the detail page holds is already masked server-side: values are the
// redaction marker, only the keys are meaningful here.
const maskedSecret: KubeObject = {
  kind: "Secret",
  type: "Opaque",
  data: { password: "**redacted**", username: "**redacted**" },
} as unknown as KubeObject;

function renderSecret() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <SecretDetail object={maskedSecret} namespace="default" name="db" />
    </QueryClientProvider>,
  );
}

beforeEach(() => revealMock.mockReset());

describe("SecretDetail", () => {
  it("masks every value by default and never ships plaintext", () => {
    renderSecret();
    expect(screen.getByText("password")).toBeInTheDocument();
    expect(screen.getByText("username")).toBeInTheDocument();
    // No revealed value present, and the redaction marker is not rendered as a value.
    expect(screen.queryByTestId("secret-value-password")).not.toBeInTheDocument();
    expect(screen.queryByText("hunter2")).not.toBeInTheDocument();
  });

  it("reveals a single key on click, fetched from the server", async () => {
    revealMock.mockResolvedValue("hunter2");
    renderSecret();

    fireEvent.click(screen.getByRole("button", { name: "Reveal password" }));

    const value = await screen.findByTestId("secret-value-password");
    expect(value).toHaveTextContent("hunter2");
    expect(revealMock).toHaveBeenCalledWith("default", "db", "password");
    // Only the requested key was fetched.
    expect(revealMock).toHaveBeenCalledOnce();
  });

  it("re-hides a revealed value", async () => {
    revealMock.mockResolvedValue("hunter2");
    renderSecret();

    fireEvent.click(screen.getByRole("button", { name: "Reveal password" }));
    await screen.findByTestId("secret-value-password");

    fireEvent.click(screen.getByRole("button", { name: "Hide password" }));
    await waitFor(() =>
      expect(screen.queryByTestId("secret-value-password")).not.toBeInTheDocument(),
    );
  });
});

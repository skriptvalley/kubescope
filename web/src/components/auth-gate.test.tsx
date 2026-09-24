import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useSignOut } from "@/hooks/use-session";
import { ApiError, type SessionState } from "@/lib/api";
import { handleAuthError, retryUnlessUnauthenticated, sessionQueryKey } from "@/lib/session";

import { AuthGate } from "./auth-gate";

const getMock = vi.hoisted(() => vi.fn());
const signInMock = vi.hoisted(() => vi.fn());
const signOutMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: { session: { get: getMock, signIn: signInMock, signOut: signOutMock } } };
});

beforeEach(() => {
  getMock.mockReset();
  signInMock.mockReset();
  signOutMock.mockReset();
});

function SignOutProbe() {
  const signOut = useSignOut();
  return (
    <button type="button" onClick={() => signOut.mutate()}>
      probe sign out
    </button>
  );
}

function renderGate() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queryClient}>
      <AuthGate>
        <div data-testid="app">app</div>
        <SignOutProbe />
      </AuthGate>
    </QueryClientProvider>,
  );
  return queryClient;
}

const signedOut: SessionState = { mode: "session", authenticated: false, usernameRequired: false };

describe("AuthGate (ADR-0013)", () => {
  it("shows the sign-in page instead of the app when signed out", async () => {
    getMock.mockResolvedValue(signedOut);
    renderGate();
    expect(await screen.findByTestId("sign-in-page")).toBeInTheDocument();
    expect(screen.queryByTestId("app")).toBeNull();
    expect(screen.queryByLabelText("Username")).toBeNull(); // password-only
  });

  it("renders the app when the session is authenticated", async () => {
    getMock.mockResolvedValue({ ...signedOut, authenticated: true });
    renderGate();
    expect(await screen.findByTestId("app")).toBeInTheDocument();
  });

  it.each(["none", "basic"] as const)("renders straight through in %s mode", async (mode) => {
    getMock.mockResolvedValue({ mode, authenticated: true, usernameRequired: false });
    renderGate();
    expect(await screen.findByTestId("app")).toBeInTheDocument();
  });

  it("signs in and then mounts the app", async () => {
    getMock.mockResolvedValue(signedOut);
    signInMock.mockResolvedValue({ ...signedOut, authenticated: true });
    renderGate();
    fireEvent.change(await screen.findByLabelText("Password"), { target: { value: "hunter2" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
    expect(await screen.findByTestId("app")).toBeInTheDocument();
    expect(signInMock.mock.calls[0][0]).toEqual({ password: "hunter2" });
  });

  it("asks for a username when the server requires one", async () => {
    getMock.mockResolvedValue({ ...signedOut, usernameRequired: true });
    signInMock.mockResolvedValue({ ...signedOut, usernameRequired: true, authenticated: true });
    renderGate();
    fireEvent.change(await screen.findByLabelText("Username"), { target: { value: "admin" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "hunter2" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
    await waitFor(() => expect(signInMock.mock.calls[0][0]).toEqual({ username: "admin", password: "hunter2" }));
  });

  it("shows an error and clears the password on a wrong password", async () => {
    getMock.mockResolvedValue(signedOut);
    signInMock.mockRejectedValue(new ApiError("wrong username or password", "invalid_credentials", 401));
    renderGate();
    const pw = await screen.findByLabelText("Password");
    fireEvent.change(pw, { target: { value: "nope" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Wrong password.");
    expect(pw).toHaveValue("");
    expect(screen.queryByTestId("app")).toBeNull();
  });

  it("signs out: shows the sign-in page and drops cached cluster data", async () => {
    getMock.mockResolvedValue({ ...signedOut, authenticated: true });
    signOutMock.mockResolvedValue(signedOut);
    const queryClient = renderGate();
    expect(await screen.findByTestId("app")).toBeInTheDocument();
    queryClient.setQueryData(["resource-list", "core", "v1", "secrets", ""], { items: ["cached"] });
    fireEvent.click(screen.getByRole("button", { name: "probe sign out" }));
    expect(await screen.findByTestId("sign-in-page")).toBeInTheDocument();
    expect(queryClient.getQueryData(["resource-list", "core", "v1", "secrets", ""])).toBeUndefined();
    expect(queryClient.getQueryData<SessionState>(sessionQueryKey)?.authenticated).toBe(false);
  });

  it("returns to the sign-in page when any API call reports the session gone", async () => {
    getMock.mockResolvedValue({ ...signedOut, authenticated: true });
    const queryClient = renderGate();
    expect(await screen.findByTestId("app")).toBeInTheDocument();
    handleAuthError(queryClient, new ApiError("sign in required", "unauthenticated", 401));
    expect(await screen.findByTestId("sign-in-page")).toBeInTheDocument();
    expect(queryClient.getQueryData<SessionState>(sessionQueryKey)?.authenticated).toBe(false);
  });
});

describe("session error policy", () => {
  it("ignores errors that are not a sign-in-required answer", () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData<SessionState>(sessionQueryKey, { ...signedOut, authenticated: true });
    handleAuthError(queryClient, new ApiError("wrong", "invalid_credentials", 401));
    handleAuthError(queryClient, new ApiError("down", "cluster_unreachable", 502));
    handleAuthError(queryClient, new Error("network"));
    expect(queryClient.getQueryData<SessionState>(sessionQueryKey)?.authenticated).toBe(true);
  });

  it("never retries a sign-in-required answer, retries others once", () => {
    const unauth = new ApiError("sign in required", "unauthenticated", 401);
    expect(retryUnlessUnauthenticated(0, unauth)).toBe(false);
    expect(retryUnlessUnauthenticated(0, new Error("x"))).toBe(true);
    expect(retryUnlessUnauthenticated(1, new Error("x"))).toBe(false);
  });
});

import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useSignOut } from "@/hooks/use-session";
import { ApiError, type SessionState } from "@/lib/api";
import { createQueryClient } from "@/lib/query-client";
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

function renderGate(queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } }), extra?: React.ReactNode) {
  render(
    <QueryClientProvider client={queryClient}>
      <AuthGate>
        <div data-testid="app">app</div>
        <SignOutProbe />
        {extra}
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

  it("signs in, confirms the cookie stuck, then mounts the app", async () => {
    getMock.mockResolvedValueOnce(signedOut).mockResolvedValue({ ...signedOut, authenticated: true });
    signInMock.mockResolvedValue({ ...signedOut, authenticated: true });
    renderGate();
    fireEvent.change(await screen.findByLabelText("Password"), { target: { value: "hunter2" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
    expect(await screen.findByTestId("app")).toBeInTheDocument();
    expect(signInMock.mock.calls[0][0]).toEqual({ password: "hunter2" });
  });

  it("asks for a username when the server requires one", async () => {
    getMock
      .mockResolvedValueOnce({ ...signedOut, usernameRequired: true })
      .mockResolvedValue({ ...signedOut, usernameRequired: true, authenticated: true });
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

  it("explains a sign-in whose cookie the browser didn't keep, instead of looping", async () => {
    getMock.mockResolvedValue(signedOut); // still signed out after a 'successful' POST
    signInMock.mockResolvedValue({ ...signedOut, authenticated: true });
    renderGate();
    fireEvent.change(await screen.findByLabelText("Password"), { target: { value: "hunter2" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/didn't keep the session cookie/);
    expect(screen.queryByTestId("app")).toBeNull();
  });

  it("names a rate-limited sign-in and keeps focus on the form", async () => {
    getMock.mockResolvedValue(signedOut);
    signInMock.mockRejectedValue(new ApiError("too many failed sign-ins", "rate_limited", 429));
    renderGate();
    const pw = await screen.findByLabelText("Password");
    fireEvent.change(pw, { target: { value: "x" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/Too many failed sign-ins/);
    expect(pw).toHaveFocus();
    expect(pw).toHaveAttribute("aria-describedby", screen.getByRole("alert").id);
  });

  it("asks for the password instead of disabling the button", async () => {
    getMock.mockResolvedValue(signedOut);
    renderGate();
    const button = await screen.findByRole("button", { name: /sign in/i });
    expect(button).toBeEnabled();
    fireEvent.click(button);
    expect(await screen.findByRole("alert")).toHaveTextContent("Enter the password.");
    expect(signInMock).not.toHaveBeenCalled();
    expect(screen.getByLabelText("Password")).toHaveFocus();
  });

  it("keeps the app when a background session refetch fails", async () => {
    getMock.mockResolvedValueOnce({ ...signedOut, authenticated: true }).mockRejectedValue(new Error("network down"));
    const queryClient = renderGate();
    expect(await screen.findByTestId("app")).toBeInTheDocument();
    void queryClient.invalidateQueries(); // e.g. a context switch
    await waitFor(() => expect(queryClient.getQueryState(sessionQueryKey)?.status).toBe("error"), { timeout: 4000 });
    expect(screen.getByTestId("app")).toBeInTheDocument();
  });

  it("stays signed in (and shows the app) when sign-out fails", async () => {
    getMock.mockResolvedValue({ ...signedOut, authenticated: true });
    signOutMock.mockRejectedValue(new Error("network down"));
    const queryClient = renderGate();
    expect(await screen.findByTestId("app")).toBeInTheDocument();
    queryClient.setQueryData(["resource-list", "core", "v1", "pods", ""], { items: ["kept"] });
    fireEvent.click(screen.getByRole("button", { name: "probe sign out" }));
    await waitFor(() => expect(signOutMock).toHaveBeenCalled());
    await waitFor(() => expect(getMock).toHaveBeenCalledTimes(2)); // re-checked the real state
    expect(screen.getByTestId("app")).toBeInTheDocument();
    expect(queryClient.getQueryData(["resource-list", "core", "v1", "pods", ""])).toEqual({ items: ["kept"] });
  });

  it("signs out: shows the sign-in page and drops cached cluster data", async () => {
    getMock.mockResolvedValue({ ...signedOut, authenticated: true });
    signOutMock.mockResolvedValue(signedOut);
    const queryClient = renderGate();
    expect(await screen.findByTestId("app")).toBeInTheDocument();
    queryClient.setQueryData(["resource-list", "core", "v1", "secrets", ""], { items: ["cached"] });
    queryClient.getMutationCache().build(queryClient, { mutationKey: ["yaml-apply"] });
    fireEvent.click(screen.getByRole("button", { name: "probe sign out" }));
    expect(await screen.findByTestId("sign-in-page")).toBeInTheDocument();
    expect(queryClient.getQueryData(["resource-list", "core", "v1", "secrets", ""])).toBeUndefined();
    expect(queryClient.getMutationCache().getAll()).toHaveLength(0);
    expect(queryClient.getQueryData<SessionState>(sessionQueryKey)?.authenticated).toBe(false);
  });

  it("the app's query client sends any 'unauthenticated' API answer back to sign-in", async () => {
    getMock.mockResolvedValue({ ...signedOut, authenticated: true });
    const failing = vi.fn().mockRejectedValue(new ApiError("sign in required", "unauthenticated", 401));
    function Probe() {
      useQuery({ queryKey: ["nodes"], queryFn: failing });
      return null;
    }
    renderGate(createQueryClient(), <Probe />);
    expect(await screen.findByTestId("sign-in-page")).toBeInTheDocument();
    expect(failing).toHaveBeenCalledTimes(1); // never retried
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

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, type SessionState, type SignInParams } from "@/lib/api";
import { CookieNotKeptError, sessionQueryKey } from "@/lib/session";

/** The server's sign-in state (ADR-0013). Fetched once; flipped to signed-out by
 *  the query client when any API call answers 401 unauthenticated. */
export function useSession() {
  return useQuery({
    queryKey: sessionQueryKey,
    queryFn: () => api.session.get(),
    staleTime: Infinity,
    retry: 1,
  });
}

/** The cached sign-in state for components inside the gate: AuthGate has already
 *  fetched it, so this only subscribes to the cache and never fetches. */
export function useSessionState(): SessionState | undefined {
  return useQuery({
    queryKey: sessionQueryKey,
    queryFn: () => api.session.get(),
    enabled: false,
    staleTime: Infinity,
  }).data;
}

/** Signs in, then confirms the cookie actually stuck before letting the gated
 *  app mount — otherwise the first API call would 401 straight back to the
 *  sign-in page in a silent loop. gcTime 0: the mutation (and the password in
 *  its variables) leaves the cache as soon as the sign-in page unmounts. */
export function useSignIn() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (params: SignInParams) => {
      await api.session.signIn(params);
      const state = await api.session.get();
      if (!state.authenticated) throw new CookieNotKeptError();
      return state;
    },
    gcTime: 0,
    onSuccess: (state) => {
      queryClient.setQueryData<SessionState>(sessionQueryKey, state);
    },
  });
}

/** Signs out. Only a confirmed sign-out flips the gate and drops every cached
 *  response and mutation (so nothing read or typed under the session lingers in
 *  memory); a failed one re-checks the real state instead of pretending. */
export function useSignOut() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => api.session.signOut(),
    onSuccess: (state) => {
      // Flip the session first (the gate swaps in the sign-in page), then drop
      // the rest. Not clear(): that would orphan the gate's own session
      // observer, which then never sees the signed-out state.
      queryClient.setQueryData<SessionState>(sessionQueryKey, state);
      queryClient.removeQueries({ predicate: (q) => q.queryKey[0] !== sessionQueryKey[0] });
      queryClient.getMutationCache().clear();
    },
    onError: () => {
      void queryClient.invalidateQueries({ queryKey: sessionQueryKey });
    },
  });
}

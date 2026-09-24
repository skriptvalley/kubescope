import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, type SessionState } from "@/lib/api";
import { sessionQueryKey } from "@/lib/session";

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

/** Signs in; on success the session cache flips and the gated app mounts. */
export function useSignIn() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: api.session.signIn,
    onSuccess: (state) => {
      queryClient.setQueryData<SessionState>(sessionQueryKey, state);
    },
  });
}

/** Signs out and drops every cached cluster response, so nothing read under the
 *  session lingers in memory behind the sign-in page. */
export function useSignOut() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: api.session.signOut,
    onSettled: (state) => {
      const prev = queryClient.getQueryData<SessionState>(sessionQueryKey);
      // Flip the session first (the gate swaps in the sign-in page), then drop
      // every other cached response. Not clear(): that would orphan the gate's
      // own session observer, which then never sees the signed-out state.
      queryClient.setQueryData<SessionState>(
        sessionQueryKey,
        state ?? { mode: "session", authenticated: false, usernameRequired: prev?.usernameRequired ?? false },
      );
      queryClient.removeQueries({ predicate: (q) => q.queryKey[0] !== sessionQueryKey[0] });
    },
  });
}

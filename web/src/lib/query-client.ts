// The app's QueryClient (main.tsx), built here so the global error wiring can be
// tested: any API answer of 401 "unauthenticated" — an expired or cleared
// session, ADR-0013 — flips the cached sign-in state and AuthGate shows the
// sign-in page; such answers are never retried.

import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";

import { handleAuthError, retryUnlessUnauthenticated } from "@/lib/session";

export function createQueryClient(): QueryClient {
  const queryClient: QueryClient = new QueryClient({
    queryCache: new QueryCache({ onError: (error) => handleAuthError(queryClient, error) }),
    mutationCache: new MutationCache({ onError: (error) => handleAuthError(queryClient, error) }),
    defaultOptions: {
      queries: {
        retry: retryUnlessUnauthenticated,
        refetchOnWindowFocus: false,
      },
    },
  });
  return queryClient;
}

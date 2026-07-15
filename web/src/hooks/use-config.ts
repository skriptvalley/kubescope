import { useQuery } from "@tanstack/react-query";

import { api, type ServerConfig } from "@/lib/api";

/** Server posture (read-only, auth mode). Not cluster-scoped — it reflects the
 *  running server, so it is fetched once and shared. The UI uses `readOnly` to
 *  disable mutation controls; the actual enforcement is server-side (ADR-0005),
 *  so a stale or missing value can never enable a mutation the server rejects. */
export function useServerConfig() {
  return useQuery({
    queryKey: ["config"],
    queryFn: api.config,
    staleTime: Infinity,
  });
}

/** Convenience: read-only flag, defaulting to true until config resolves so
 *  controls never flash enabled before we know the server posture. */
export function useReadOnly(): boolean {
  const { data } = useServerConfig();
  return (data as ServerConfig | undefined)?.readOnly ?? true;
}

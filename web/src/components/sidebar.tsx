import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, RefreshCw } from "lucide-react";
import { NavLink } from "react-router-dom";

import { Skeleton } from "@/components/ui/skeleton";
import { useDiscovery } from "@/hooks/use-discovery";
import { useSetupState } from "@/hooks/use-setup";
import { api, ApiError } from "@/lib/api";
import { buildNav } from "@/lib/discovery-nav";
import { cn } from "@/lib/utils";

const pinned = [
  { to: "/overview", label: "Overview" },
  { to: "/nodes", label: "Nodes" },
  { to: "/events", label: "Events" },
];

export function Sidebar() {
  const { data, isPending, isError, error } = useDiscovery();
  const { data: setup } = useSetupState();
  const queryClient = useQueryClient();
  // Explicit refresh: force a server-side re-discovery (picks up a CRD
  // installed after startup) and replace the cached result.
  const refresh = useMutation({
    mutationFn: () => api.resources.discovery(true),
    onSuccess: (fresh) => queryClient.setQueryData(["discovery"], fresh),
  });

  const groups = buildNav(data);

  return (
    <aside className="w-64 shrink-0 overflow-y-auto border-r">
      <nav className="space-y-6 p-4">
        <Section title="Cluster">
          {pinned.map((item) => (
            <NavItem key={item.to} to={item.to} label={item.label} />
          ))}
        </Section>

        <div>
          <div className="mb-2 flex items-center justify-between">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              Resources
            </h3>
            <button
              type="button"
              onClick={() => refresh.mutate()}
              disabled={refresh.isPending}
              aria-label="Refresh resource types"
              title="Refresh resource types"
              className="text-muted-foreground hover:text-foreground disabled:opacity-50"
            >
              <RefreshCw className={cn("h-3.5 w-3.5", refresh.isPending && "animate-spin")} />
            </button>
          </div>

          {isPending ? (
            <div className="space-y-2" data-testid="sidebar-loading">
              {[0, 1, 2, 3, 4].map((i) => (
                <Skeleton key={i} className="h-5 w-full" />
              ))}
            </div>
          ) : isError ? (
            // FB-9: before the server has a usable cluster (setup not ready, or
            // still loading), discovery failing is expected, not an error — show a
            // muted placeholder. A genuine mid-session failure (setup ready) is red.
            setup?.state === "ready" ? (
              <p className="text-xs text-destructive">
                {error instanceof ApiError ? error.message : "Discovery failed"}
              </p>
            ) : (
              <p className="text-xs text-muted-foreground" data-testid="sidebar-waiting">
                Waiting for a cluster connection…
              </p>
            )
          ) : (
            <div className="space-y-4">
              {data?.warnings && data.warnings.length > 0 && (
                <p
                  className="flex items-center gap-1 text-xs text-muted-foreground"
                  title={data.warnings.join("\n")}
                >
                  <AlertTriangle className="h-3.5 w-3.5" />
                  Some API groups unavailable
                </p>
              )}
              {groups.map((group) => (
                <div key={group.name || "core"}>
                  <p className="mb-1 text-xs font-medium text-muted-foreground">{group.label}</p>
                  <ul>
                    {group.resources.map((r) => (
                      <li key={r.key}>
                        <NavItem to={r.to} label={r.label} title={r.resource} />
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          )}
        </div>
      </nav>
    </aside>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <h3 className="mb-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
        {title}
      </h3>
      <div>{children}</div>
    </div>
  );
}

function NavItem({ to, label, title }: { to: string; label: string; title?: string }) {
  return (
    <NavLink
      to={to}
      title={title}
      className={({ isActive }) =>
        cn(
          "block truncate rounded-sm px-2 py-1 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground",
          isActive && "bg-accent font-medium text-accent-foreground",
        )
      }
    >
      {label}
    </NavLink>
  );
}

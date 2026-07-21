import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Activity, AlertTriangle, LayoutGrid, RefreshCw, Server } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { NavLink } from "react-router-dom";

import { Skeleton } from "@/components/ui/skeleton";
import { useResourceCounts } from "@/hooks/use-counts";
import { useDiscovery } from "@/hooks/use-discovery";
import { useSetupState } from "@/hooks/use-setup";
import { api, ApiError } from "@/lib/api";
import { buildNav } from "@/lib/discovery-nav";
import { cn } from "@/lib/utils";

const pinned: { to: string; label: string; Icon: LucideIcon }[] = [
  { to: "/overview", label: "Overview", Icon: LayoutGrid },
  { to: "/nodes", label: "Nodes", Icon: Server },
  { to: "/events", label: "Events", Icon: Activity },
];

export function Sidebar() {
  const { data, isPending, isError, error } = useDiscovery();
  const { data: setup } = useSetupState();
  const { data: countData } = useResourceCounts();
  const queryClient = useQueryClient();
  // Explicit refresh: force a server-side re-discovery (picks up a CRD
  // installed after startup) and replace the cached result.
  const refresh = useMutation({
    mutationFn: () => api.resources.discovery(true),
    onSuccess: (fresh) => queryClient.setQueryData(["discovery"], fresh),
  });

  const groups = buildNav(data);
  const counts = countData?.counts;

  return (
    <aside className="w-[216px] shrink-0 overflow-y-auto border-r border-sidebar-border bg-sidebar text-sidebar-foreground">
      <nav className="flex flex-col gap-[18px] p-3">
        <div className="flex flex-col gap-px">
          {pinned.map((item) => (
            <PinnedItem key={item.to} to={item.to} label={item.label} Icon={item.Icon} />
          ))}
        </div>

        <div>
          <div className="mb-1 flex items-center justify-between px-2">
            <p className="text-[11px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
              Resources
            </p>
            <button
              type="button"
              onClick={() => refresh.mutate()}
              disabled={refresh.isPending}
              aria-label="Refresh resource types"
              title="Refresh resource types"
              className="text-muted-foreground transition-colors hover:text-foreground disabled:opacity-50"
            >
              <RefreshCw className={cn("h-3.5 w-3.5", refresh.isPending && "animate-spin")} />
            </button>
          </div>

          {isPending ? (
            <div className="space-y-1.5" data-testid="sidebar-loading">
              {[0, 1, 2, 3, 4].map((i) => (
                <Skeleton key={i} className="h-5 w-full" />
              ))}
            </div>
          ) : isError ? (
            // FB-9: before the server has a usable cluster (setup not ready, or
            // still loading), discovery failing is expected, not an error — show a
            // muted placeholder. A genuine mid-session failure (setup ready) is red.
            setup?.state === "ready" ? (
              <p className="px-2 text-xs text-destructive">
                {error instanceof ApiError ? error.message : "Discovery failed"}
              </p>
            ) : (
              <p className="px-2 text-xs text-muted-foreground" data-testid="sidebar-waiting">
                Waiting for a cluster connection…
              </p>
            )
          ) : (
            <div className="space-y-4">
              {data?.warnings && data.warnings.length > 0 && (
                <p
                  className="flex items-center gap-1 px-2 text-xs text-muted-foreground"
                  title={data.warnings.join("\n")}
                >
                  <AlertTriangle className="h-3.5 w-3.5" />
                  Some API groups unavailable
                </p>
              )}
              {groups.map((group) => (
                <div key={group.name || "core"}>
                  <p className="mb-1 px-2 text-[11px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
                    {group.label}
                  </p>
                  <ul className="flex flex-col gap-px">
                    {group.resources.map((r) => (
                      <li key={r.key}>
                        <NavItem to={r.to} label={r.label} title={r.resource} count={counts?.[r.key]} />
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

const navItemBase =
  "flex items-center gap-2 rounded-sm px-2 text-[13px] no-underline transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground hover:no-underline";

function activeClass(isActive: boolean): string {
  return isActive
    ? "bg-sidebar-accent font-medium text-sidebar-primary"
    : "text-sidebar-foreground";
}

function PinnedItem({ to, label, Icon }: { to: string; label: string; Icon: LucideIcon }) {
  return (
    <NavLink
      to={to}
      className={({ isActive }) => cn(navItemBase, "py-[5px]", activeClass(isActive))}
    >
      <Icon className="h-[15px] w-[15px] shrink-0" aria-hidden="true" />
      <span className="flex-1 truncate">{label}</span>
    </NavLink>
  );
}

function NavItem({
  to,
  label,
  title,
  count,
}: {
  to: string;
  label: string;
  title?: string;
  count?: number;
}) {
  return (
    <NavLink
      to={to}
      title={title}
      className={({ isActive }) => cn(navItemBase, "py-1", activeClass(isActive))}
    >
      <span className="flex-1 truncate">{label}</span>
      {count !== undefined && (
        <span className="font-mono text-[10.5px] text-muted-foreground">{count}</span>
      )}
    </NavLink>
  );
}

import { Lock } from "lucide-react";
import { useSyncExternalStore } from "react";
import { Link, Outlet } from "react-router-dom";

import { ActiveForwardsPanel } from "@/components/active-forwards-panel";
import { ClusterBanner } from "@/components/cluster-banner";
import { ContextSwitcher } from "@/components/context-switcher";
import { GlobalSearch } from "@/components/global-search";
import { ShortcutsHelp } from "@/components/shortcuts-help";
import { Sidebar } from "@/components/sidebar";
import { ThemeToggle } from "@/components/theme-toggle";
import wordmark from "@/assets/skriptvalley-wordmark.png";
import { useServerConfig } from "@/hooks/use-config";
import { useSetupState } from "@/hooks/use-setup";
import { connectivity } from "@/lib/connectivity";
import { StarterPage } from "@/pages/starter";

export function Layout() {
  return (
    <div className="flex h-screen flex-col">
      <header className="shrink-0 border-b bg-card">
        <div className="flex h-[52px] items-center gap-3 px-4">
          <Link to="/overview" className="flex shrink-0 items-center gap-2.5 no-underline hover:no-underline">
            <img src={wordmark} alt="Skript Valley" className="block h-[26px] w-auto" />
            <span className="h-5 w-px bg-border" aria-hidden="true" />
            <span className="font-display text-[15px] font-semibold tracking-[-0.01em] text-foreground">
              Kubescope
            </span>
          </Link>
          <ContextSwitcher />
          <div className="ml-auto flex min-w-0 flex-1 items-center justify-end gap-2.5">
            <GlobalSearch />
            <ShortcutsHelp />
            <ThemeToggle />
          </div>
        </div>
      </header>
      <ReadOnlyBanner />
      <ClusterBanner />
      <div className="flex min-h-0 flex-1">
        <Sidebar />
        <main className="min-w-0 flex-1 overflow-y-auto px-6 py-5">
          <SetupGate />
        </main>
      </div>
      <ActiveForwardsPanel />
    </div>
  );
}

/** Renders the full-page starter instead of the routed page when the server has
 *  no usable cluster (FB-6). The first three states always gate; an unreachable
 *  active context only gates before the first successful connection — once
 *  connected, a later outage is handled in-app by ClusterBanner, not the starter.
 *  While setup is still loading (nothing cached) the routed page renders and
 *  shows its own skeleton/error. */
function SetupGate() {
  const { data } = useSetupState();
  const everConnected = useSyncExternalStore(
    connectivity.subscribe,
    connectivity.hasEverConnected,
  );

  if (data) {
    const { state } = data;
    if (
      state === "no_kubeconfig" ||
      state === "no_contexts" ||
      state === "no_active_context" ||
      (state === "active_unreachable" && !everConnected)
    ) {
      return <StarterPage state={data} />;
    }
  }

  return <Outlet />;
}

/** A persistent notice when the server runs in read-only mode, so the absence of
 *  mutation controls is explained rather than mysterious (ADR-0005). */
function ReadOnlyBanner() {
  const { data } = useServerConfig();
  if (!data?.readOnly) return null;
  return (
    <div
      role="status"
      className="flex shrink-0 items-center gap-2 border-b border-highlight/30 bg-highlight/10 px-4 py-2 text-sm text-badge-hl-fg"
    >
      <Lock className="h-4 w-4" />
      <span>
        Read-only mode — mutating actions (edit, scale, restart, delete, cordon, drain) are disabled.
        Set <code className="font-mono text-xs">KUBESCOPE_READ_ONLY=false</code> to enable them.
      </span>
    </div>
  );
}

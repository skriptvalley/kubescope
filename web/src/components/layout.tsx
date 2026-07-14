import { Outlet } from "react-router-dom";

import { ContextSwitcher } from "@/components/context-switcher";
import { Sidebar } from "@/components/sidebar";

export function Layout() {
  return (
    <div className="flex h-screen flex-col">
      <header className="shrink-0 border-b">
        <div className="flex h-14 items-center gap-6 px-4">
          <span className="text-lg font-semibold tracking-tight">Kubescope</span>
          <div className="ml-auto">
            <ContextSwitcher />
          </div>
        </div>
      </header>
      <div className="flex min-h-0 flex-1">
        <Sidebar />
        <main className="min-w-0 flex-1 overflow-y-auto px-6 py-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

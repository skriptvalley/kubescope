import { NavLink, Outlet } from "react-router-dom";

import { ContextSwitcher } from "@/components/context-switcher";
import { cn } from "@/lib/utils";

const navItems = [
  { to: "/overview", label: "Overview" },
  { to: "/nodes", label: "Nodes" },
];

export function Layout() {
  return (
    <div className="min-h-screen">
      <header className="border-b">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-6 px-4">
          <span className="text-lg font-semibold tracking-tight">Kubescope</span>
          <nav className="flex items-center gap-4 text-sm">
            {navItems.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                className={({ isActive }) =>
                  cn(
                    "text-muted-foreground transition-colors hover:text-foreground",
                    isActive && "font-medium text-foreground",
                  )
                }
              >
                {item.label}
              </NavLink>
            ))}
          </nav>
          <div className="ml-auto">
            <ContextSwitcher />
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-4 py-6">
        <Outlet />
      </main>
    </div>
  );
}

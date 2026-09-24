import { QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { createBrowserRouter, Navigate, RouterProvider } from "react-router-dom";

import { AuthGate } from "@/components/auth-gate";
import { Layout } from "@/components/layout";
import { routerBasename } from "@/lib/base";
import { createQueryClient } from "@/lib/query-client";
import { EventsPage } from "@/pages/events";
import { NodesPage } from "@/pages/nodes";
import { OverviewPage } from "@/pages/overview";
import { ResourceDetailPage } from "@/pages/resource-detail";
import { ResourceListPage } from "@/pages/resource-list";

// Self-hosted fonts (ADR-0002/0009): Space Grotesk (headings), Geist (body),
// Geist Mono (identifiers). Bundled by Vite into the embedded binary — no CDN.
import "@fontsource/space-grotesk/latin-500.css";
import "@fontsource/space-grotesk/latin-600.css";
import "@fontsource/space-grotesk/latin-700.css";
import "@fontsource-variable/geist/wght.css";
import "@fontsource-variable/geist-mono/wght.css";

import "./index.css";

// Global 401-"unauthenticated" handling lives in createQueryClient (ADR-0013).
const queryClient = createQueryClient();

const router = createBrowserRouter([
  {
    path: "/",
    element: (
      <AuthGate>
        <Layout />
      </AuthGate>
    ),
    children: [
      { index: true, element: <Navigate to="/overview" replace /> },
      { path: "overview", element: <OverviewPage /> },
      { path: "nodes", element: <NodesPage /> },
      { path: "events", element: <EventsPage /> },
      // Generic resource engine. Namespaced objects deep-link with their
      // namespace segment; cluster-scoped ones without.
      { path: "resources/:group/:version/:resource", element: <ResourceListPage /> },
      {
        path: "resources/:group/:version/:resource/:namespace/:name",
        element: <ResourceDetailPage />,
      },
      { path: "resources/:group/:version/:resource/:name", element: <ResourceDetailPage /> },
    ],
  },
  // Served under a sub-path (KUBESCOPE_BASE_PATH), routes and links resolve
  // beneath it (ADR-0012); "/" at the root or when reached without the prefix.
], { basename: routerBasename() });

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);

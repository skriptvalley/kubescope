import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { createBrowserRouter, Navigate, RouterProvider } from "react-router-dom";

import { Layout } from "@/components/layout";
import { NodesPage } from "@/pages/nodes";
import { OverviewPage } from "@/pages/overview";
import { ResourceDetailPage } from "@/pages/resource-detail";
import { ResourceListPage } from "@/pages/resource-list";

import "./index.css";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});

const router = createBrowserRouter([
  {
    path: "/",
    element: <Layout />,
    children: [
      { index: true, element: <Navigate to="/overview" replace /> },
      { path: "overview", element: <OverviewPage /> },
      { path: "nodes", element: <NodesPage /> },
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
]);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);

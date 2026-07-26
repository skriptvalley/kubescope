import cytoscape from "cytoscape";
import fcose from "cytoscape-fcose";
import { Info, Maximize2, ZoomIn, ZoomOut } from "lucide-react";
import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { useNavigate } from "react-router-dom";

import { EmptyState } from "@/components/empty-state";
import { ErrorState } from "@/components/error-state";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { DEFAULT_GRAPH_DEPTH, useResourceGraph } from "@/hooks/use-graph";
import { toCytoscapeElements } from "@/lib/graph-elements";
import { oklchToRenderColor, type RenderColor } from "@/lib/oklch";
import { themeStore } from "@/lib/theme";
import { cn } from "@/lib/utils";

// Resource relationship graph view (FB-14, ADR-0011). Cytoscape.js with the
// fcose layout renders the compound nodes — a Deployment's box holding its
// ReplicaSet, pods and Service — which is the requirement React Flow could not
// meet without hand-rolling nesting (see the ADR).
//
// Both libraries are bundled by Vite, never fetched (ADR-0002); the whole view
// is lazy-loaded by its caller so the graph's weight stays off first paint.

// fcose registers itself on the cytoscape singleton, so guard against the
// double-register that React's StrictMode remount would otherwise cause.
let layoutRegistered = false;
function registerLayout() {
  if (layoutRegistered) return;
  layoutRegistered = true;
  try {
    cytoscape.use(fcose);
  } catch {
    // Already registered by another bundle instance — harmless.
  }
}

/** Depths the control offers. The server clamps anything larger and says so. */
const DEPTHS = [1, 2, 3, 4, 5];

interface ResourceGraphProps {
  namespace: string;
  kind: string;
  name: string;
}

export default function ResourceGraph({ namespace, kind, name }: ResourceGraphProps) {
  const [depth, setDepth] = useState(DEFAULT_GRAPH_DEPTH);
  const graph = useResourceGraph(namespace, kind, name, depth);

  const elements = useMemo(
    () => (graph.data ? toCytoscapeElements(graph.data) : []),
    [graph.data],
  );
  // The canvas cannot read CSS variables, so the palette is resolved from the
  // document and rebuilt whenever the theme flips (ADR-0009).
  const theme = useSyncExternalStore(themeStore.subscribe, themeStore.get, themeStore.get);
  const isEmpty = Boolean(graph.data && graph.data.nodes.length <= 1 && graph.data.edges.length === 0);

  return (
    <div className="flex flex-col gap-3" data-testid="resource-graph">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-[13px] text-muted-foreground">
          Relationships around this {kind} in{" "}
          <span className="font-mono text-xs text-foreground">{namespace}</span>
          {graph.data && ` · ${graph.data.nodes.length} objects`}
        </p>
        <label className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">Depth</span>
          <select
            aria-label="Graph depth"
            value={depth}
            onChange={(e) => setDepth(Number(e.target.value))}
            className={cn(
              "h-8 rounded-md border border-input bg-background px-2 text-sm",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            )}
          >
            {DEPTHS.map((d) => (
              <option key={d} value={d}>
                {d}
              </option>
            ))}
          </select>
        </label>
      </div>

      {graph.data?.partial && (
        <Alert data-testid="graph-partial">
          <Info className="h-4 w-4" />
          <AlertTitle>Partial graph</AlertTitle>
          <AlertDescription>
            <ul className="list-disc space-y-0.5 pl-4 text-xs">
              {(graph.data.notes ?? []).map((note) => (
                <li key={note}>{note}</li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      )}

      {graph.isPending ? (
        <Skeleton className="h-[420px] w-full" data-testid="graph-loading" />
      ) : graph.isError ? (
        <ErrorState error={graph.error} onRetry={() => graph.refetch()} title="Failed to build the graph" />
      ) : isEmpty ? (
        <EmptyState message={`Nothing is linked to this ${kind} within ${depth} hop${depth === 1 ? "" : "s"}.`}>
          <p className="mt-1 text-xs">Raise the depth to widen the search.</p>
        </EmptyState>
      ) : (
        <>
          <GraphCanvas elements={elements} theme={theme} />
          <Legend />
        </>
      )}
    </div>
  );
}

/** The Cytoscape surface. Kept separate so the chrome above renders (and is
 *  testable) without a live renderer. */
function GraphCanvas({ elements, theme }: { elements: ReturnType<typeof toCytoscapeElements>; theme: string }) {
  const container = useRef<HTMLDivElement>(null);
  const cy = useRef<cytoscape.Core | null>(null);
  const navigate = useNavigate();
  // Held in a ref so the click handler always sees the current navigate without
  // tearing down and rebuilding the graph on every render.
  const navigateRef = useRef(navigate);
  navigateRef.current = navigate;

  useEffect(() => {
    if (!container.current) return;
    registerLayout();

    let instance: cytoscape.Core;
    try {
      instance = cytoscape({
        container: container.current,
        elements,
        style: duskStylesheet(),
        // Wheel zoom would hijack page scrolling inside a tab, so zooming is on
        // the explicit controls below; panning and node dragging stay free.
        userZoomingEnabled: false,
        layout: {
          name: "fcose",
          animate: false,
          nodeSeparation: 90,
          idealEdgeLength: 110,
          nodeRepulsion: 9000,
          padding: 24,
        } as cytoscape.LayoutOptions,
      });
    } catch {
      // A renderer that cannot start (no canvas support) leaves the container
      // empty rather than breaking the surrounding detail view.
      return;
    }

    instance.on("tap", "node", (event) => {
      const href = event.target.data("href");
      if (typeof href === "string" && href) navigateRef.current(href);
    });
    cy.current = instance;

    return () => {
      cy.current = null;
      instance.destroy();
    };
  }, [elements, theme]);

  const zoomBy = (factor: number) => {
    const instance = cy.current;
    if (!instance) return;
    instance.zoom({ level: instance.zoom() * factor, renderedPosition: centerOf(instance) });
  };

  return (
    <div className="relative">
      <div
        ref={container}
        data-testid="graph-canvas"
        className="h-[420px] w-full rounded-lg border bg-card"
        role="img"
        aria-label="Resource relationship graph"
      />
      <div className="absolute right-2 top-2 flex flex-col gap-1">
        <CanvasButton label="Zoom in" onClick={() => zoomBy(1.25)}>
          <ZoomIn className="h-3.5 w-3.5" />
        </CanvasButton>
        <CanvasButton label="Zoom out" onClick={() => zoomBy(0.8)}>
          <ZoomOut className="h-3.5 w-3.5" />
        </CanvasButton>
        <CanvasButton label="Fit to view" onClick={() => cy.current?.fit(undefined, 24)}>
          <Maximize2 className="h-3.5 w-3.5" />
        </CanvasButton>
      </div>
    </div>
  );
}

/** Zooming around the viewport centre keeps the focus roughly in place, rather
 *  than pulling the graph towards the origin. */
function centerOf(instance: cytoscape.Core): { x: number; y: number } {
  return { x: instance.width() / 2, y: instance.height() / 2 };
}

function CanvasButton({
  label,
  onClick,
  children,
}: {
  label: string;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      className={cn(
        "flex h-7 w-7 items-center justify-center rounded-md border border-input bg-background/90",
        "text-muted-foreground transition-colors hover:bg-muted hover:text-foreground",
        "focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring",
      )}
    >
      {children}
    </button>
  );
}

const FALLBACK: RenderColor = { color: "#888888", opacity: 1 };

/** Reads a Dusk token off the document as a renderer-ready colour + opacity.
 *  Both halves matter: the OKLCH the rest of the app uses has to be converted
 *  (see lib/oklch), and Cytoscape wants alpha as a separate property rather than
 *  inside the colour string. */
function token(name: string, alpha = 1): RenderColor {
  if (typeof document === "undefined") return FALLBACK;
  const raw = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  if (!raw) return FALLBACK;
  return oklchToRenderColor(raw, alpha);
}

/** Tone → the same Dusk families the status badges use (ADR-0009): ok is brand
 *  teal, progress is highlight pumpkin, warn is destructive red, neutral muted. */
function toneColor(tone: string): RenderColor {
  switch (tone) {
    case "ok":
      return token("--brand");
    case "progress":
      return token("--highlight");
    case "warn":
      return token("--destructive");
    default:
      return token("--muted-foreground", 0.55);
  }
}

/** The Cytoscape stylesheet, resolved against the current theme's Dusk tokens.
 *  Kind is carried by shape and health by the outline tone, so the graph reads
 *  before any label does. */
function duskStylesheet(): cytoscape.StylesheetJson {
  const foreground = token("--foreground");
  const muted = token("--muted-foreground");
  const card = token("--card");
  const primary = token("--primary");
  // --border is a hairline meant for dividers (10% white in dark); as a node
  // outline it would be invisible, so quiet nodes use a dimmed muted instead.
  const line = token("--muted-foreground", 0.4);

  const outline = (tone: string) => {
    const { color, opacity } = toneColor(tone);
    return { "border-color": color, "border-opacity": opacity };
  };
  const stroke = (c: RenderColor) => ({
    "line-color": c.color,
    "line-opacity": c.opacity,
    "target-arrow-color": c.color,
  });

  return [
    {
      selector: "node",
      style: {
        "background-color": card.color,
        "background-opacity": card.opacity,
        "border-width": 1.5,
        ...outline("neutral"),
        label: "data(label)",
        color: foreground.color,
        "font-size": 10,
        "font-family": "Geist Variable, ui-sans-serif, system-ui, sans-serif",
        "text-valign": "bottom",
        "text-margin-y": 5,
        "text-wrap": "ellipsis",
        "text-max-width": "110px",
        width: 34,
        height: 34,
      },
    },
    { selector: "node.category-workload", style: { shape: "round-rectangle", width: 42, height: 30 } },
    { selector: "node.category-pod", style: { shape: "ellipse" } },
    { selector: "node.category-network", style: { shape: "round-diamond", width: 38, height: 38 } },
    { selector: "node.category-config", style: { shape: "round-tag", width: 38, height: 28 } },
    { selector: "node.category-storage", style: { shape: "barrel", width: 38, height: 30 } },
    { selector: "node.category-identity", style: { shape: "round-hexagon" } },
    { selector: "node.category-other", style: { shape: "round-octagon" } },

    { selector: "node.tone-ok", style: outline("ok") },
    { selector: "node.tone-progress", style: outline("progress") },
    { selector: "node.tone-warn", style: outline("warn") },
    { selector: "node.tone-neutral", style: outline("neutral") },

    {
      selector: "node.focus",
      style: {
        "border-width": 3,
        "border-color": primary.color,
        "border-opacity": 1,
        "font-weight": 600,
      },
    },
    { selector: "node.aggregate", style: { "border-style": "double", "border-width": 4 } },
    { selector: "node.missing", style: { "border-style": "dashed", color: muted.color } },

    {
      selector: "node.group",
      style: {
        shape: "round-rectangle",
        "background-color": primary.color,
        "background-opacity": 0.08,
        "border-color": primary.color,
        "border-opacity": 0.35,
        "border-width": 1,
        label: "data(label)",
        color: muted.color,
        "font-size": 10,
        "font-weight": 600,
        "text-valign": "top",
        "text-halign": "center",
        "text-margin-y": -4,
        padding: "18px",
      },
    },

    {
      selector: "edge",
      style: {
        width: 1.2,
        ...stroke(line),
        "target-arrow-shape": "triangle",
        "arrow-scale": 0.7,
        "curve-style": "bezier",
        label: "data(label)",
        "font-size": 8,
        color: muted.color,
        "text-background-color": card.color,
        "text-background-opacity": 0.85,
        "text-background-padding": "2px",
      },
    },
    { selector: "edge.relation-owns", style: stroke(token("--primary", 0.6)) },
    { selector: "edge.relation-routes", style: stroke(token("--brand", 0.8)) },
    { selector: "edge.relation-scales", style: stroke(token("--highlight", 0.8)) },
    {
      selector: "edge.relation-mounts, edge.relation-env, edge.relation-imagePullSecret",
      style: { "line-style": "dashed" },
    },
    { selector: "edge.relation-serviceAccount", style: { "line-style": "dotted" } },
  ] as cytoscape.StylesheetJson;
}

const LEGEND: { label: string; className: string }[] = [
  { label: "Workload", className: "rounded-[3px] bg-muted-foreground/40" },
  { label: "Pod", className: "rounded-full bg-muted-foreground/40" },
  { label: "Network", className: "rotate-45 bg-brand/60" },
  { label: "Config", className: "rounded-[2px] bg-muted-foreground/30" },
];

function Legend() {
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[11px] text-muted-foreground">
      {LEGEND.map((item) => (
        <span key={item.label} className="inline-flex items-center gap-1.5">
          <span className={cn("h-2.5 w-2.5", item.className)} aria-hidden="true" />
          {item.label}
        </span>
      ))}
      <span className="inline-flex items-center gap-1.5">
        <span className="h-2.5 w-2.5 rounded-full ring-2 ring-primary" aria-hidden="true" />
        Focus
      </span>
      <span>Border colour follows status · click a node to open it</span>
    </div>
  );
}


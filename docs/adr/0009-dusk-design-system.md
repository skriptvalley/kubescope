# 0009. Adopt the skriptvalley "Dusk" design system

- **Status:** Accepted
- **Date:** 2026-07-21

## Context

Kubescope v0.1.0 ships a functional but generic shadcn-zinc UI. The product is
part of the **skriptvalley** family, which has its own visual identity — "Dusk":
violet primary, teal `brand`, pumpkin `highlight`, cream/aubergine surfaces;
Space Grotesk headings + Geist body + Geist Mono for every Kubernetes
identifier; light **and** dark with a real theme toggle. Adopting a named design
system over the locked shadcn-zinc default is a locked-decision change
(`CLAUDE.md` rule #3), so it needs an ADR before code (FB-11).

Two constraints shape *how* it lands:

- **Offline binary (0002):** the FE is embedded and runs with no network, so the
  design's `fonts.googleapis.com` links cannot ship — the fonts must be
  self-hosted and bundled by Vite.
- **shadcn/ui is locked (0001):** this is a *re-theme*, not a framework swap.
  Tailwind + the shadcn primitives, the component structure, and the entire data
  layer (routes, hooks, TanStack Query, SSE live updates, read-only gating,
  secret masking) stay exactly as they are.

The design also surfaces three data enhancements gated behind toggles
(`sidebarCounts`, `quotaBars`, CPU/Memory). The owner opted **in** to all three
this session, so this ADR also records the metrics-server data source they
require.

## Decision

**Tokens — OKLCH, Tailwind-native.** The token layer moves from shadcn's HSL
triplets (`hsl(var(--x))`) to the Dusk **OKLCH** palette. To keep every Tailwind
opacity modifier working (`bg-destructive/5`, `bg-brand/15`, `hover:bg-primary/90`
— relied on across the shadcn primitives), palette tokens are stored as
space-separated OKLCH **channels** (`--primary: 0.470 0.135 300`) and mapped in
`tailwind.config.js` as `oklch(var(--x) / <alpha-value>)`. This is the faithful
Tailwind-v3 realization of the design's raw-`oklch()` + `color-mix` opacity: the
`<alpha-value>` slot injects the alpha the design expresses with `color-mix`.
`border`/`input`/`sidebar-border` (which carry their own alpha in dark mode) and
the computed `--ring-soft` / `--badge-*-fg` tokens are stored as full-color vars.
New semantic colors beyond the shadcn set: `brand`, `highlight`, `sidebar*`,
`chart-1..3`, `badge-brand-fg` / `badge-hl-fg` (light uses the dark
`*-foreground` for legible text on a tint; dark uses the bright base — an
intentional light/dark swap), `ring-soft`. Radii retune to cards 14 / controls
10 / badges 8.

**Fonts — self-hosted.** Space Grotesk (500/600/700), Geist and Geist Mono are
vendored via `@fontsource/space-grotesk` and `@fontsource-variable/geist` /
`@fontsource-variable/geist-mono`, imported in `main.tsx` so Vite fingerprints
and bundles the woff2 into the embedded binary. The production build makes **no**
`fonts.googleapis.com`/external request (verified by grepping `dist/`).

**Theme toggle — light / system / dark.** A header segmented radiogroup persists
to `localStorage` (`kubescope-theme`); `system` follows
`matchMedia('(prefers-color-scheme: dark)')`. A tiny inline script in
`index.html` applies the stored theme before first paint (no flash);
`lib/theme.ts` is the runtime store the toggle reads/writes.

**Status → tone, centralized.** One `tone()` mapping
(Running→`brand`, Pending/Init/Creating→`highlight`,
Crash/Failed/`*Error`/Evicted→`destructive`, else `muted`; restart thresholds
0 / 1–5 / >5) drives every badge, dot, ready/restart color, and condition chip —
`status-badge.tsx` and `workload-status.ts` route through it.

**Data enhancements (owner opted in).** All three toggled features are built:
- **CPU / Memory** read from **metrics-server** via the **dynamic client**
  against `metrics.k8s.io/v1beta1` — *no new Go module dependency* (consistent
  with 0003's generic dynamic-client access). Metrics are strictly best-effort:
  when metrics-server is absent the endpoint reports unavailable and the columns
  render `—`, never an error.
- **Sidebar per-resource counts** and **namespace quota bars** are extra typed
  reads (per-type list counts; `ResourceQuota` per namespace), best-effort and
  degrading cleanly (no count / hidden section) when unavailable.

**What does NOT change:** Tailwind + shadcn primitives, component structure, all
routes/hooks/TanStack Query/SSE, read-only server-side gating, secret masking. No
new environment variable. No new Go dependency (metrics use the existing dynamic
client). New `web/` dependencies: the three `@fontsource*` font packages.

## Consequences

**Positive:**
- One coherent skriptvalley identity across every screen, light and dark.
- Opacity modifiers keep working unchanged, so the re-theme is genuinely
  presentation-only — non-restyled screens inherit the new look with zero code
  change.
- Metrics via the dynamic client add CPU/Memory with **no** new module and a
  clean absent-server degrade path.
- A single `tone()` source removes the scattered per-view status color logic.

**Negative:**
- OKLCH channels + `<alpha-value>` is a less obvious token format than raw
  `oklch()`; documented here and in `index.css`.
- All Geist subsets (latin/cyrillic/greek/…) bundle into the binary (~150 KB of
  woff2) since the variable packages ship one `wght.css`. Accepted for
  simplicity and correctness; trimming to latin-only is a future option.
- Counts/metrics/quota reads add per-render cluster calls; all are best-effort,
  bounded, and cache via TanStack Query.

## Alternatives considered

- **Map Tailwind colors to bare `var(--token)`** (the literal reading of the
  design) — rejected. Verified that Tailwind v3.4 then **drops** every opacity
  modifier (`bg-destructive/5` emits no rule), silently regressing shadcn
  primitives and non-restyled screens. The `<alpha-value>` channel form is the
  correct realization.
- **Add the `k8s.io/metrics` typed client for CPU/Memory** — rejected. A new Go
  module for two usage numbers; the dynamic client already reaches
  `metrics.k8s.io` and matches 0003.
- **Non-variable Geist + per-subset latin CSS to shrink the bundle** — deferred.
  The variable packages are simpler; the size delta is small for an offline tool.
- **Ship the design's `fonts.googleapis.com` links** — rejected outright; breaks
  the offline-binary guarantee (0002).

# Feedback — Dusk UI redesign (skriptvalley design system)

> Feedback-driven prompt (FB-11), not part of the canonical Sprint 0–8 plan.
> Treat as a self-contained mini-sprint. Branch `fix/dusk-ui-redesign`.
> One session; PR per story (or grouped); do not pull unrelated work.

## Context recap
Read before starting (in order):
1. `STATUS.md` — FB-11; the app is v0.1.0, all Sprint 0–8 screens exist and are live-wired.
2. `docs/design/dusk-ui/SPEC.md` — **the design spec** (tokens, tone rule, screen inventory,
   deltas). Open `docs/design/dusk-ui/Kubescope-Dusk-UI.dc.html` in a browser for the pixel
   reference and read its `<script type="text/x-dc">` for exact tone thresholds + sample data.
   Brand assets in `docs/design/dusk-ui/brand/`.
3. ADRs: `0001` (tech stack — shadcn/ui locked; this **re-themes**, doesn't drop it),
   `0002` (single offline binary embedding the FE — **fonts must be self-hosted, no CDN**).
   Write **ADR-0009** first (this prompt, Story A).
One sprint per session. Rules in `CLAUDE.md` apply (rule #3: the design-system swap is a
locked-decision change → ADR before code).

## Why (the trigger)
The current UI is functional shadcn-zinc. Kubescope should wear the **skriptvalley “Dusk”**
system: violet primary, teal brand, pumpkin highlight, cream/aubergine surfaces; Space Grotesk
headings + Geist body + Geist Mono identifiers; light **and** dark with a real theme toggle.
This is a presentation-layer redesign over the **existing** data layer (routes, hooks,
TanStack Query, SSE, read-only gating, secret masking all unchanged) plus a theme toggle and a
few decision-gated data enhancements.

## Goal
Every screen renders in the Dusk system in both light and dark, dense and responsive, reusing
the system’s Button / Card / Badge / Input / Select / Table / Dialog — no bespoke one-off
styles — with zero regression to behavior, live updates, or guardrails.

## Scope decision (confirm at session start)
Three items need real backend data and are **not** free — default is to keep them out of the
core redesign and log as follow-ups unless the user opts in:
- **CPU / Memory columns** (pods table): need **metrics-server** — a v2-backlog item + new
  dependency/architecture. If wanted → ADR addendum + typed metrics endpoint; else drop the two
  columns (design already renders `—` when absent).
- **Live sidebar per-resource counts** and **namespace quota bars**: extra backend reads
  (per-type list counts; `ResourceQuota`). Default: defer (render without counts / hide quota
  section) or bring in under Story F.
Design gates all three behind toggles (`sidebarCounts`, `quotaBars`, CPU/Mem) so the core
redesign is complete without them.

## Stories

### Story A — Dusk foundation: tokens, fonts, theme toggle + ADR-0009
Rewrite the token layer and add theming. This is the base everything else builds on.
**Acceptance criteria:**
- [ ] `index.css` tokens replaced with the Dusk **OKLCH** set (light `:root` + `.dark`) incl. new
  `--brand`/`--highlight`/`--sidebar*`/`--chart-*`/`--badge-*-fg`/`--ring-soft` (SPEC has the full values, incl. the light/dark `badge-*-fg` swap); radii retuned (cards 14 / controls 10 / badges 8).
- [ ] `tailwind.config.js` color mappings switched from `hsl(var(--x))` to **`var(--x)`**; brand/highlight/sidebar/chart scales + fonts (`Space Grotesk`, `Geist`, `Geist Mono`) added.
- [ ] All three fonts **self-hosted** and bundled (e.g. `@fontsource/*`) — verify the production build makes **no** `fonts.googleapis.com`/external request (ADR-0002).
- [ ] Header **theme toggle** (segmented light / system / dark), persisted to `localStorage`, `system` following `matchMedia('(prefers-color-scheme: dark)')`; toggles `.dark` on `<html>`.
- [ ] **ADR-0009** written (adopt skriptvalley Dusk over shadcn-zinc: OKLCH tokens, vendored fonts, theme toggle; what stays: Tailwind + shadcn primitives, component structure, all data/behavior). ADR index + `STATUS.md` + `CLAUDE.md` stack note updated (no new env var; note `web/` font deps).
- [ ] Regression: every existing screen still renders under the new tokens; `make fe-test` + lint + `tsc` green.

### Story B — App shell, sidebar, context switcher, search
**Acceptance criteria:**
- [ ] Header (52px): Skript Valley wordmark (`docs/design/dusk-ui/brand/wordmark.png` → vendor into `web/`) + divider + “Kubescope” (Space Grotesk), then switcher · search · theme toggle. Restyle `layout.tsx`.
- [ ] Sidebar 216px: pinned Overview/Nodes/Events (icons) + discovery-driven API-group sections, Dusk active styling. **Keep** the FB-9 muted non-ready states and discovery refresh.
- [ ] Context switcher menu: per-context **health badges** (teal Healthy / red Unreachable) + “Manage kubeconfig sources” (reuse FB-8 surface). Global search: `/` kbd hint + Dusk results dropdown. Behavior + a11y (radiogroup/aria) unchanged.

### Story C — Cluster overview
**Acceptance criteria:**
- [ ] Title + subtitle (mono ctx · version · Live) + Refresh; **stat cards** Nodes / Pods / Namespaces / Health from real overview data (Health = Degraded/red when workloads failing); **attention banner** derived from real failing-workload state (if the overview API lacks pod-health aggregation, add a minimal typed summary — do not fabricate).
- [ ] Pods table (TanStack Table) Dusk-styled with the three filters and the status **tone** rule; row → pod detail; live updates intact. CPU/Memory columns only if Scope-decision opts in.

### Story D — Pod & namespace detail
**Acceptance criteria:**
- [ ] Pod detail Dusk-styled: breadcrumb, header + status badge + Live + Delete, tabs (reuse existing Logs/Terminal/YAML), summary `<dl>`, containers table, conditions chips, port-forward form (reuse existing controls), events list. Reuse existing detail hooks/components — restyle only.
- [ ] Namespace detail Dusk-styled: fields, labels chips, pods table. Quota bars only if Scope-decision opts in (else hide the section).

### Story E — Status tone + confirm dialog
**Acceptance criteria:**
- [ ] Centralize the Dusk `tone()` mapping (Running→brand, Pending/Init/Creating→highlight, Crash/Failed/Error/Evicted→destructive, else muted; restart 0/1–5/>5 thresholds) and route `status-badge.tsx` / `workload-status.ts` through it — one source of truth for every badge, dot, ready/restart color, condition chip.
- [ ] `confirm-dialog.tsx` restyled to the Dusk dialog; add an optional cascade-warning slot for namespace deletes (“Everything in `<ns>` is deleted — N pods, …”). Typed-name gate + read-only gating behavior byte-for-byte unchanged; tests updated.

### Story F — (OPTIONAL, only if Scope-decision opts in) data enhancements
**Acceptance criteria:**
- [ ] CPU/Mem: typed metrics endpoint (metrics-server) + columns; **graceful `—`** and no error when metrics-server is absent. Requires ADR-0009 addendum (or its own ADR).
- [ ] Sidebar counts and/or namespace quota bars from real reads; degrade cleanly when unavailable. Anything not taken this session is logged as its own FB item in `STATUS.md`.

## Testing requirements
- FE (vitest): theme toggle (persist + system/matchMedia), token/regression render of existing components, the centralized `tone()` mapping, restyled dialog/table, switcher health badges, FB-9 muted states preserved.
- Build: `make fe-build` + `make build`; **grep the built bundle for external font/network references — there must be none** (offline binary).
- Visual smoke: run the app against kind, verify each screen in **light and dark** (drive refetches via query invalidation/curl — the in-app browser reports `visibilityState:"hidden"`, pausing interval polling). Backend enhancements (Story F): manual kind smoke incl. the metrics-server-absent path.

## Definition of Done
- Compiles/builds; `make test` (if backend touched) + `make lint` + `make fe-test` + `tsc` green; visual smoke light+dark.
- ADR-0009 written; `docs/ARCHITECTURE.md` FE note + `CLAUDE.md` stack row updated if changed.
- `STATUS.md` updated (FB-11 progress; ADR recorded; any deferred enhancement logged as a new FB).

## End-of-session actions
1. `make fe-test`, `make lint`, `tsc` (+ `make test` if backend touched); build + bundle grep.
2. Update `STATUS.md` (last work `[feedback]`, next expected, checkboxes, ADR-0009).
3. Commit (Conventional Commits), push `fix/dusk-ui-redesign`, open PR; agent-review the diff and fix findings.
4. Gates + CI green → squash-merge, sync `main`; concise summary (outcome + blockers only).

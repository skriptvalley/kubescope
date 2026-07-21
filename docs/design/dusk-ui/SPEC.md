# Kubescope — Dusk UI design spec

Distilled, implementation-facing spec for the **Dusk** redesign (skriptvalley design
system). Companion to the pixel source in this folder.

## Source

- **Pixel reference:** [`Kubescope-Dusk-UI.dc.html`](Kubescope-Dusk-UI.dc.html) — the
  self-contained design bundle (inline tokens + markup + a `<script type="text/x-dc">`
  with the sample data and status-tone logic). Open it in a browser to see every
  screen; read the `<script>` block for the exact tone/threshold rules.
- **Live design:** claude.ai/design project `a4650167-fc6a-43f7-8cbd-9e7215ee3301`,
  file `Kubescope - Dusk UI.dc.html`. Re-pull with the `DesignSync` tool
  (`get_file`) or open in a logged-in browser. A sibling `Kubescope - Current UI.dc.html`
  is a faithful recreation of today's UI for side-by-side comparison.
- **Brand:** [`brand/wordmark.png`](brand/wordmark.png) (Skript Valley horizontal
  wordmark, 1192×440 RGBA) and [`brand/lockup.png`](brand/lockup.png) (square mark,
  1563×1563). The shell renders the wordmark at `height:26px`.

## Design tokens (OKLCH)

The design uses **raw OKLCH** values applied as `var(--token)` directly. ⚠️ Today's
app stores HSL triplets and wraps them (`hsl(var(--token))`) in `tailwind.config.js`
+ `index.css` — adopting Dusk means switching the token **format** and dropping the
`hsl()` wrapper. New semantic colors beyond the shadcn set: `brand` (teal),
`highlight` (pumpkin), `sidebar*`, `chart-1..3`, `badge-*-fg`, `ring-soft`.

### Light (`:root`)
```css
--background:oklch(0.984 0.012 100);   --foreground:oklch(0.255 0.045 300);
--card:oklch(1 0 0);                    --card-foreground:oklch(0.255 0.045 300);
--popover:oklch(1 0 0);                 --popover-foreground:oklch(0.255 0.045 300);
--primary:oklch(0.470 0.135 300);       --primary-foreground:oklch(0.990 0.010 100);
--secondary:oklch(0.955 0.018 100);     --secondary-foreground:oklch(0.320 0.050 300);
--muted:oklch(0.955 0.018 100);         --muted-foreground:oklch(0.520 0.025 95);
--accent:oklch(0.945 0.024 100);        --accent-foreground:oklch(0.320 0.050 300);
--destructive:oklch(0.577 0.245 27.325);
--border:oklch(0.910 0.020 98);         --input:oklch(0.910 0.020 98);  --ring:oklch(0.470 0.135 300);
--brand:oklch(0.704 0.118 178);         --brand-foreground:oklch(0.270 0.045 178);
--highlight:oklch(0.715 0.175 52);      --highlight-foreground:oklch(0.300 0.080 52);
--chart-1:oklch(0.470 0.135 300); --chart-2:oklch(0.715 0.175 52); --chart-3:oklch(0.704 0.118 178);
--sidebar:oklch(0.975 0.015 100);       --sidebar-foreground:oklch(0.255 0.045 300);
--sidebar-primary:oklch(0.470 0.135 300); --sidebar-accent:oklch(0.945 0.024 100);
--sidebar-accent-foreground:oklch(0.320 0.050 300); --sidebar-border:oklch(0.910 0.020 98);
--badge-brand-fg:var(--brand-foreground); --badge-hl-fg:var(--highlight-foreground);
--ring-soft:color-mix(in oklch, var(--foreground) 10%, transparent);
```

### Dark (`.dark`)
```css
--background:oklch(0.165 0.022 305);   --foreground:oklch(0.940 0.015 95);
--card:oklch(0.215 0.026 305);         --card-foreground:oklch(0.940 0.015 95);
--popover:oklch(0.215 0.026 305);      --popover-foreground:oklch(0.940 0.015 95);
--primary:oklch(0.585 0.150 300);      --primary-foreground:oklch(0.990 0.010 100);
--secondary:oklch(0.270 0.028 305);    --secondary-foreground:oklch(0.940 0.015 95);
--muted:oklch(0.270 0.028 305);        --muted-foreground:oklch(0.720 0.022 95);
--accent:oklch(0.305 0.030 305);       --accent-foreground:oklch(0.940 0.015 95);
--destructive:oklch(0.704 0.191 22.216);
--border:oklch(1 0 0 / 10%);           --input:oklch(1 0 0 / 14%);  --ring:oklch(0.585 0.150 300);
--brand:oklch(0.720 0.118 178);        --brand-foreground:oklch(0.200 0.040 178);
--highlight:oklch(0.740 0.165 55);     --highlight-foreground:oklch(0.220 0.070 52);
--chart-1:oklch(0.640 0.160 300); --chart-2:oklch(0.740 0.165 55); --chart-3:oklch(0.720 0.118 178);
--sidebar:oklch(0.200 0.024 305);      --sidebar-foreground:oklch(0.940 0.015 95);
--sidebar-primary:oklch(0.585 0.150 300); --sidebar-accent:oklch(0.305 0.030 305);
--sidebar-accent-foreground:oklch(0.940 0.015 95); --sidebar-border:oklch(1 0 0 / 10%);
--badge-brand-fg:var(--brand);         --badge-hl-fg:var(--highlight);
--ring-soft:color-mix(in oklch, oklch(1 0 0) 12%, transparent);
```
> Note the light/dark **swap** of `--badge-brand-fg` / `--badge-hl-fg`: light mode uses
> the darker `*-foreground` for legible text on tint; dark mode uses the bright base.

## Typography

- **Space Grotesk** (500/600/700) — headings, stat values, section titles. `letter-spacing:-0.02em` on page/stat headings.
- **Geist** (400/500/600) — body / UI default, `font-size:14px`, antialiased.
- **Geist Mono** (400/500) — every Kubernetes identifier: resource names, namespaces, contexts, IPs, images, ages, counts, restart counts, the `/` search hint.
- ⚠️ **Offline constraint (ADR-0002):** Kubescope embeds the built FE and runs as a
  single offline binary. The design's `fonts.googleapis.com` links **cannot** ship —
  self-host all three families (e.g. `@fontsource/space-grotesk`, `@fontsource/geist-sans`,
  `@fontsource/geist-mono`) so they bundle into the Vite build. No network font fetch.

## Shape / elevation

- **Radii:** cards/panels/dialog `14px`; buttons/inputs/selects/nav items/search `10px`;
  badges/chips `8px`; pills & status dots `9999px`.
- **Cards** carry no border — a hairline ring instead: `background:var(--card); box-shadow:0 0 0 1px var(--ring-soft)`.
- **Focus ring** (inputs/search): `border-color:var(--ring); box-shadow:0 0 0 3px color-mix(in oklch, var(--ring) 40%, transparent)`. Delete-confirm input focuses `--destructive` instead.
- **Popovers/menus:** `border:1px solid var(--border); background:var(--popover); box-shadow:0 10px 30px rgb(0 0 0/0.18); animation:fadeIn 0.12s`.
- **Dialog:** overlay `rgb(0 0 0/0.5)`; panel `max-width:448px`, `box-shadow:0 10px 30px rgb(0 0 0/0.25)`, `animation:dlgIn 0.15s`.
- Header `52px`. Sidebar `216px`. Dense: 32px controls, 7px table row padding, 12–13px body text.

## Status → tone (the load-bearing rule)

One function drives every status badge, dot, ready/restart color, condition chip, and
stat accent. Reproduce it faithfully (see the `tone()` and threshold logic in the source `<script>`):

| Status class | Token family | Applies to |
|---|---|---|
| `Running` | **brand** (teal) — `color-mix(--brand 15%)` bg, `--badge-brand-fg` text, `--brand` dot | Running pods/containers, Active ns, Healthy ctx, Live indicator |
| `Pending` · `Init:*` · `ContainerCreating` | **highlight** (pumpkin) — `color-mix(--highlight 15%)` | pending/creating |
| `CrashLoopBackOff` · `Failed` · `*Error` · `Evicted` | **destructive** (red) — `color-mix(--destructive 10%)` | failing; Unreachable ctx; Warning events |
| else (`Completed`, unknown) | **muted** | terminal/neutral |

Other conventions: **Ready** cell foreground = normal if ready else muted. **Restarts**:
`0`→muted, `1–5`→`--badge-hl-fg`, `>5`→`--destructive` + weight 600. Namespace/pod-link
cells are muted, hovering to `--primary`.

## Screens (all present in the source)

1. **App shell** — header: Skript Valley wordmark │ divider │ "Kubescope" (Space Grotesk) ·
   **cluster switcher** (dot + mono ctx + up/down chevrons → menu of contexts with per-context
   health badges *Healthy*/​*Unreachable* + a "Manage kubeconfig sources" item) · **search**
   (magnifier + `/` kbd hint + results dropdown, opens at ≥2 chars) · **theme toggle**
   (segmented radiogroup sun/monitor/moon = light/system/dark).
2. **Cluster overview** — `h1` + subtitle (mono ctx · version · Live) + Refresh button;
   **attention banner** (destructive-tinted, "N workload failing — …", Inspect button);
   **stat cards** grid (`auto-fit minmax(185px,1fr)`): Nodes / Pods / Namespaces / Health,
   big Space-Grotesk value + dot-badges (Health = "Degraded" red when failing); **pods table**
   card with filters (namespace select, status select, name input) and columns
   Name · Namespace · Ready · Status · Restarts · **CPU · Memory** · Node · Age. Row → pod detail.
3. **Pod detail** — breadcrumb (Pods › ns › name); title + status badge + Live + Delete;
   tabs Summary / Logs / Terminal / YAML (underline-active); Summary = fields `<dl>`
   (Phase, Node, Pod IP, QoS, Controlled by [primary link], Age), Containers table
   (Container/State/Ready/Restarts/Image), Conditions chips, **Port forwarding** form
   (pod-port + local-port inputs + Forward button + declared ports), Events list
   (Warning=red badge / Normal=outline; reason · count · message · age).
4. **Namespace detail** — breadcrumb; title + Active badge + Delete-namespace; fields `<dl>`
   (Status/Age/Pods/Services/Workloads); Labels chips (mono); **Resource quota** section —
   quota bars (name · used/hard · progress bar `--chart-1`); Pods-in-namespace table.
5. **Delete Dialog** — the typed-name gate (already in `components/confirm-dialog.tsx`).
   Title + description; for namespace a destructive-tinted **cascade warning** box
   ("Everything in `payments` is deleted — 4 pods, 3 services, …"); "Type `<name>` to confirm"
   + mono input; Cancel + Delete (disabled until `typed === name`).

## Deltas from today's app + constraints

| Area | Change |
|---|---|
| Tokens | HSL-triplet + `hsl()` wrap → **raw OKLCH**; add `brand`/`highlight`/`sidebar*`/`chart-*`/`badge-*-fg`/`ring-soft`; retune radii (14/10/8). Rewire `tailwind.config.js` color mappings to `var(--token)`. |
| Fonts | Add + **self-host** Space Grotesk / Geist / Geist Mono (no CDN — offline binary). |
| Theme | **New** light/system/dark toggle in header, persisted (`localStorage`) + `matchMedia` for system; today only `.dark` tokens exist, no switcher. |
| Shell/sidebar/switcher/search | Restyle existing `layout.tsx` / `sidebar.tsx` / `context-switcher.tsx` / `global-search.tsx`; sidebar → `216px`, per-resource **counts**, API-group headings. Keep discovery-driven nav + FB-9 muted states + "Manage kubeconfig sources". |
| Status badges | Route existing `status-badge.tsx` / `workload-status.ts` through the Dusk `tone()` mapping above. |
| Confirm dialog | Restyle `confirm-dialog.tsx` to the Dusk dialog; add the optional cascade-warning slot for namespace deletes. |
| **CPU / Memory columns** | ⚠️ Need **metrics-server** (usage data) — this is a v2-backlog item + new dependency/architecture. Not free. Decide: drop the columns, or bring metrics forward under a new ADR. Design renders `—` when metrics are absent. |
| **Sidebar counts / quota bars** | Extra backend reads (per-type counts; ResourceQuota per namespace). Decide whether in-scope now or a follow-up; the design gates both behind toggles (`sidebarCounts`, `quotaBars`). |

Everything else (routes, hooks, TanStack Query, SSE live updates, read-only gating,
secret masking) is unchanged — this is a presentation-layer redesign over the existing
data layer, plus the theme toggle and the (decision-gated) data enhancements.

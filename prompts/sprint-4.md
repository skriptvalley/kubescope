# Sprint 4 — Live updates + logs + events

## Context recap
Read before starting (in order):
1. `STATUS.md` — current state + any feedback tasks.
2. `docs/ARCHITECTURE.md` — component you are touching.
3. ADRs: `docs/adr/0006-live-updates-sse-and-streaming-websocket.md` (SSE for watch/log fan-out; WebSocket is reserved for exec in Sprint 6 — do not use it here).
One sprint per session. Do not pull work forward. Rules in `CLAUDE.md` apply.

## Sprint goal
Make the dashboard feel live: resource lists and details auto-update on cluster changes, pod logs stream in real time, and a live events feed exists.

## Stories
### Story 4.1 — Watch→SSE bridge (informers, per-context fan-out, reconnect handling)
Build the watch bridge in `internal/stream`: shared informers per context+GVK feed an SSE endpoint; multiple subscribers fan out from one informer; watch drops trigger resync, not crashes.
**Acceptance criteria:**
- [ ] SSE endpoint streams add/update/delete events for a requested GVK (optionally namespace-filtered) in the active context.
- [ ] One informer per context+GVK is shared across all subscribers and torn down when the last subscriber disconnects.
- [ ] apiserver watch errors trigger re-list/resync; clients receive an explicit resync event and the process never crashes.
- [ ] Periodic SSE heartbeat comments keep idle connections alive through proxies.
- [ ] Switching context closes streams bound to the previous context.

### Story 4.2 — Live-updating lists/details in UI (SSE consumption → TanStack Query cache updates)
A shared FE subscription hook consumes the SSE stream and patches the TanStack Query cache in place — no full refetch per event.
**Acceptance criteria:**
- [ ] Resource list pages update rows on add/update/delete events without a full list refetch.
- [ ] Detail views update live when the viewed object changes; deletion of the viewed object is surfaced.
- [ ] SSE drop triggers reconnect with backoff; a resync event invalidates the affected query.
- [ ] UI shows a live/stale connection indicator; polling fallback applies when SSE is unavailable.

### Story 4.3 — Pod log streaming (follow, container select, previous, tail lines)
SSE log endpoint wrapping the pod log API, plus a log viewer on Pod detail.
**Acceptance criteria:**
- [ ] Log endpoint streams with follow and supports container, previous, and tailLines parameters.
- [ ] UI log viewer: container selector, follow toggle, tail-lines input, previous-logs toggle.
- [ ] Auto-scroll while following; pauses when the user scrolls up, resumable.
- [ ] Stream end (container exit, pod deletion) is surfaced as a closed state, not a silent hang.

### Story 4.4 — Events feed (cluster-wide + per-namespace)
Dedicated events page backed by the watch→SSE bridge, live-updating and filterable.
**Acceptance criteria:**
- [ ] Events page lists cluster-wide events with namespace and type (Normal/Warning) filters.
- [ ] New events appear live via the Story 4.1 bridge without manual refresh.
- [ ] Columns: type, reason, involved object, message, count, last-seen age.
- [ ] Event rows deep-link to the involved object's detail view when that object exists.

## Task checklist
- [ ] Implement informer manager in `internal/stream` (per context+GVK, ref-counted lifecycle).
- [ ] Implement SSE handler: event serialization, namespace filtering, heartbeats, resync events.
- [ ] Handle watch errors with re-list/resync; tear down streams on context switch.
- [ ] Implement pod log SSE endpoint (follow, container, previous, tailLines).
- [ ] Register stream routes in `internal/server`.
- [ ] FE: SSE subscription hook with reconnect/backoff and TanStack Query cache patching.
- [ ] FE: wire live updates into generic list pages and detail views.
- [ ] FE: live/stale indicator + polling fallback.
- [ ] FE: log viewer component on Pod detail (controls + auto-scroll behavior).
- [ ] FE: events page with filters and live updates.
- [ ] Table-driven Go tests for fan-out/subscription bookkeeping and log parameter mapping.
- [ ] envtest coverage for watch event delivery and resync behavior.
- [ ] vitest coverage for the SSE hook, cache patching, and log viewer.
- [ ] Manual kind smoke (see Testing requirements).

## Testing requirements
| Layer | Must cover |
|---|---|
| Table-driven Go tests | Informer ref-counting (subscribe/unsubscribe/teardown), SSE event serialization, log endpoint parameter mapping (container/previous/tailLines) |
| envtest | Add/update/delete events reach an SSE subscriber; resync after a forced watch error; namespace filtering |
| vitest | SSE hook reconnect/backoff, cache patched without refetch, deletion handling, log viewer controls + scroll-pause |
| Manual kind smoke | Scale a Deployment and watch the list update untouched; stream logs from a running pod and `previous` from a crashing pod; watch the events feed while deleting a pod |

## Definition of Done
- Compiles/builds; lint clean.
- Unit tests for new logic pass.
- Manual smoke against kind for cluster-touching features.
- Docs updated if behavior/API changed.

## End-of-session actions
1. Run `make test` and `make lint`.
2. Update `STATUS.md` (last work + type, next expected, checkboxes).
3. Commit (Conventional Commits), push branch `sprint-4/<story-slug>`, open PR.
4. Print a concise summary: outcome + blockers only.

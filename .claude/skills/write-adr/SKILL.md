---
name: write-adr
description: Author a new ADR — numbering, MADR template, status lifecycle, index and STATUS.md bookkeeping.
---

# Write an ADR

Required whenever a session adds a dependency, changes architecture, or alters a locked decision (golden rule in `CLAUDE.md`).

## Steps

1. **Next number** — list `docs/adr/`, take the highest `NNNN` and add 1 (zero-padded to 4 digits).
2. **Filename** — `docs/adr/NNNN-kebab-title.md`, short kebab-case title (e.g. `0007-metrics-adapter-seam.md`).
3. **Write the MADR sections** using the template below: Status / Date / Context / Decision / Consequences / Alternatives considered. One decision per ADR.
4. **Status lifecycle** — `Proposed` → `Accepted` → `Superseded by <NNNN>`. Never delete an ADR. To change a past decision, write a new ADR and update the old one's Status line to `Superseded by <new NNNN>` — never edit the old Decision text.
5. **Update the index** — add a row (number, title, status) to the table in `docs/adr/README.md`; update the superseded ADR's row if applicable.
6. **Record in STATUS.md** — set "ADRs touched this session" to the number(s) added or changed.

## Template

Identical to the canonical template in `docs/adr/README.md` — keep the two in sync.

```markdown
# <NNNN>. <Title>

- **Status:** Proposed | Accepted | Superseded by 000X
- **Date:** YYYY-MM-DD

## Context
<What forces this decision? Constraints, requirements, prior art.>

## Decision
<What we are doing, stated as fact.>

## Consequences
**Positive:**
- <benefit>

**Negative:**
- <cost / risk we accept>

## Alternatives considered
- **<Alternative>** — <why rejected>
```

## Rules

- Concise: an ADR is typically under one page.
- Cross-link related ADRs and the component in `docs/ARCHITECTURE.md` the decision lands in (relative link from `docs/adr/`: `../ARCHITECTURE.md`).
- Date is the day the ADR was written, not the day of the discussion.

## Checklist

- [ ] Next free number used, filename `NNNN-kebab-title.md`
- [ ] All MADR sections filled, one decision only
- [ ] Superseded ADR (if any) re-statused, not edited
- [ ] `docs/adr/README.md` index row added/updated
- [ ] STATUS.md "ADRs touched this session" set

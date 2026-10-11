# Operational Brain

The Operational Brain is HAI's owner-scoped system map. It combines current projections from the knowledge graph, context memory, connected sources, pursuits, workflows, registered agents, and advisory teams without creating another source of truth.

## Boundaries

- Read endpoints require an authenticated recognized role with read permission.
- Write endpoints require write permission and reuse HAI's existing memory and knowledge persistence.
- The graph grants no execution, approval, tool, or external-effect authority.
- Agent boot context applies the strictest agent, team, membership, and role ceilings.
- Secret-like metadata keys are removed before projection.
- Snapshots and traversals are bounded; configure the snapshot ceiling with `OPERATIONAL_GRAPH_MAX_NODES`.
- Read projections use a short owner-scoped cache (`OPERATIONAL_GRAPH_CACHE_SECONDS`, default 5, maximum 60) and invalidate after governed graph writes. Set it to `0` when immediate external projection refresh is required.
- The A2A planning bridge does not expose operational graph records, source evidence, memory, or credentials.
- No additional service, model, database, container, polling worker, or JavaScript runtime is required.

## API

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/operational-graph/snapshot` | Bounded current projection and quality counts |
| `GET` | `/api/v1/operational-graph/search` | Ranked exact, prefix, and substring search |
| `GET` | `/api/v1/operational-graph/nodes/:id/neighborhood` | Bounded breadth-first neighborhood |
| `GET` | `/api/v1/operational-graph/path?from=...&to=...` | Shortest bounded relationship path |
| `GET` | `/api/v1/operational-graph/agents/:id/boot` | Effective agent capabilities and prohibitions |
| `POST` | `/api/v1/operational-graph/memories` | Owner-scoped context memory write |
| `POST` | `/api/v1/operational-graph/reports` | Local-only report event requiring review |

The Angular route is `/operational-brain`. Basic view shows attention and system health. Advanced sections expose layers, agent context, and projection quality; the per-module disclosure state uses HAI's existing versioned local preference service.

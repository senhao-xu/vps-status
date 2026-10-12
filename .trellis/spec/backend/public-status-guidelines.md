# Public Status Boundary

## 1. Scope / Trigger

Use this contract for public-page configuration, node publication, any
unauthenticated status endpoint, and frontend public status rendering. It
prevents administrator node models, host identity, credentials, and retained
history from crossing the public boundary by accident.

## 2. Signatures

Storage owns:

```go
func (*Store) PublicPageConfig(context.Context) (PublicPage, error)
func (*Store) UpdatePublicPage(context.Context, PublicPage, time.Time) error
func (*Store) ListPublishedNodes(context.Context) ([]Node, error)
```

HTTP owns:

```text
GET /api/v1/admin/public-page
PUT /api/v1/admin/public-page
GET /api/v1/public/status
```

Control schema v7 owns `public_page(singleton_id=1, enabled, title,
description, updated_at)` and `public_nodes(node_id, published_at)`.

## 3. Contracts

- Nodes are private by default. Publication requires an active node ID in the
  complete administrator-supplied `node_ids` allowlist.
- `PUT` is authenticated and CSRF-protected. It replaces presentation fields
  and the allowlist in one `control.db` transaction.
- The unauthenticated DTO is a separate type, never `nodeDTO` with fields
  removed. Top-level fields are exactly `page` and `nodes`.
- Public node fields are exactly display `name`, `group`, `online`, current
  uptime/CPU/memory/disk/RX/TX, and current UTC-month RX/TX totals.
- Offline/unavailable/degraded values are JSON `null`. Public status never
  fails merely because `telemetry.db` is unavailable.
- The public response never contains stable internal node IDs, hostname, IP,
  OS, kernel, architecture, virtualization, credential material, probe
  targets, notification data, or history points.

## 4. Validation & Error Matrix

| Condition | Result |
|---|---|
| Empty/overlong title, description over 512 bytes, duplicate/missing/deleted node | `400 invalid_input`; transaction unchanged |
| Admin mutation without session | `401 unauthorized` |
| Admin mutation without matching CSRF | `403 csrf_invalid` |
| Public page disabled | `200` with page settings and an empty node array |
| Telemetry unavailable | Live public fields continue; month totals are `null` |
| Node deletion | `public_nodes` row is explicitly removed in the node-deletion transaction (nodes are soft-deleted, so an FK cascade alone is insufficient) |

## 5. Good / Base / Bad Cases

- Good: an administrator explicitly publishes two nodes and only their display
  aliases/current allowlisted metrics appear publicly.
- Base: public page is fresh/disabled and returns no nodes.
- Bad: serializing `nodeDTO`, then deleting `hostname` in the browser. A later
  administrator field addition would silently become public.

## 6. Tests Required

- Fresh schema proves `public_nodes` is empty and the page is disabled.
- Replacement is atomic; missing/duplicate nodes leave the old config intact.
- v6→v7 backup contains neither v7 table.
- API tests use sentinel hostname, OS, kernel, architecture/IP-like fact and
  credential values and assert all are absent, along with `history`, `points`,
  `bucket_start`, and cumulative report counters.
- Frontend decoder rejects every unknown/forbidden public key; rendering tests
  assert online state has text and missing values do not become measured zero.

## 7. Wrong vs Correct

### Wrong

```go
writeJSON(w, http.StatusOK, api.nodeDTO(node)) // administrator model
```

### Correct

```go
dto := publicNodeDTO{Name: node.Name, Group: node.Group}
projectPublicReport(&dto, *snapshot.Report) // explicit allowlist only
writeJSON(w, http.StatusOK, dto)
```

---
id: "0031"
title: Mutation Input Errors Fail Closed
status: accepted
created: 2026-11-18
summary: Mutations rejected for invalid input are surfaced, never queued as local tickets; labels resolve to UUIDs from the local catalog.
---

# 0031 · Mutation Input Errors Fail Closed

Related: [BACKLOG.md](../BACKLOG.md), [principles.md](../principles.md), [ADR 0025](0025-json-read-contract.md).

## Context

`ticket create --labels a,b` sent label **names** to Plane, which expects UUIDs and answered HTTP 400. `TrackerService.Create` treated any remote failure as "offline", saved a `PROJ-LOCAL-n` ticket and returned success; the 400 only surfaced later in `alo sync`, and replays failed forever. `ticket update` discarded every remote error (`_ =`). The legacy Python CLI resolved labels from the cached `labels` table (`get_labels_map`); the Go port kept the catalog (`ListLabels`, `SyncLabels`) but stopped using it on mutations.

## Decision

1. **Labels resolve in `TrackerService`, from the local catalog.** Names (case-insensitive) or UUIDs map to UUIDs via `store.ListLabels`; the catalog is refreshed with `SyncLabels` only when a name is missing. `RemoteTrackerClient` receives UUIDs and stays a thin HTTP client. The local cache and the outbox payload keep the names the user typed.
2. **Unknown label = error, not warning.** After a refresh, a still-unknown name returns `issue.UnknownLabelError` listing the valid ones. (Legacy Python warned on `stderr` and dropped the label; `--json` requires `stderr` to stay empty and a dropped label is a silent data loss.)
3. **Rejections are not transient.** The Plane adapter returns `issue.RejectedError` for HTTP 400, 404, 409 and 422. `Create` and `Update` propagate it (`issue.IsInputError`) instead of queueing; `Update` checks before writing the local cache. 401, 429 and 5xx remain transient and follow local-first.
4. **Offline stays local-first.** If a name is missing from the catalog and the refresh fails, nothing is sent; the mutation is queued with names and the outbox replay resolves them. A replay that still cannot resolve is marked failed, visible in `alo ticket outbox`.
5. **CLI contract.** Input errors exit `2` with category `user_input` and a hint (`alo ticket labels -p <PROJECT> --refresh`).

## Consequences

- A mistyped label or a 4xx from Plane now fails the command immediately, with the valid options in the message.
- Extends the existing fail-closed pre-flight for unknown projects (no poison pills in the outbox).
- `ticket update` still ignores *transient* remote failures without queueing them; tracked in [BACKLOG.md](../BACKLOG.md).

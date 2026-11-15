---
id: "0030"
title: Background Sync Daemon (alo daemon)
status: proposed
created: 2026-11-15
summary: Non-blocking background worker for automatic periodic outbox flushing and health pings upon network availability.
---

# 0030 · Background Sync Daemon (`alo daemon`)

Related: [BACKLOG.md](../BACKLOG.md), [principles.md](../principles.md), RFC [0027](./0027-incremental-sync-and-granularity.md), Decision [0028](../decisions/0028-cqs-delta-sync-watermarks.md).

## Context

Allostasis operates under an offline-first architecture. Mutating commands (`alo log`, `alo ticket create`, `alo ticket comment add`) enqueue changes into the local SQLite `outbox` table when offline. Currently, flushing requires manual invocation of `alo sync`.

## Proposal

Introduce an optional background worker (`alo daemon`) or `systemd --user` service unit:
1. **Periodic Flush & Exponential Backoff:**
   - Periodically inspects local SQLite outbox queue for pending records.
   - Pings upstream tracker health endpoint when pending mutations exist.
   - Flushes batches idempotently using existing `TrackerService.FlushOutbox`.
   - Employs exponential backoff with jitter on network or server errors.
2. **System Integration:**
   - Lightweight POSIX process with low memory footprint and Wazero pure-Go runtime.
   - Responds gracefully to `SIGTERM` / `SIGINT` completing in-flight transactions.
   - Provides optional `systemd --user` configuration template (`alo daemon install-systemd`).

## Consequences

- Offline mutations synchronize seamlessly in the background without manual developer intervention.
- The daemon remains strictly optional; headless CLI and manual `alo sync` remain 100% functional without running the daemon.

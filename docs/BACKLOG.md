# Engineering Backlog

Open technical considerations, pending defects, and deferred improvements for Allostasis (`alo`).

---

## 1. Known Defects

*(None pending — all regression and concurrency tests passing with zero data races).*

---

## 2. Deferred Enhancements & Open RFCs

- **RFC-0014 · Pluggable Multi-Tracker Drivers (Linear / GitHub Issues):**
  - **Status:** Architecture ready; adapter development open.
  - **Context:** The canonical port [`RemoteTrackerClient`](file:///home/johantv/Code/allostasis/internal/app/ports.go#L44-L49) in `internal/app/ports.go` abstracts all remote tracking operations. Plane CE is 100% functional. Implementations for Linear GraphQL and GitHub Issues REST can be plugged in without modifying application services or domain logic.

- **RFC-0029 · Interactive TUI Worktree Navigator (`alo ws browse`):**
  - **Status:** Proposed for v0.2.0.
  - **Context:** Expand Bubble Tea presentation with an interactive worktree container explorer:
    - Visual container list with health color indicators (green: clean/active; yellow: stale/abandoned; blue: merged/ready to prune).
    - Quick actions: `<Enter>` opens a terminal or Zellij tab in the container; `<p>` triggers an interactive safe prune modal with `.tar.gz` backup confirmation; `<c>` opens an interactive creation form.

- **RFC-0030 · Background Sync Daemon (`alo daemon`):**
  - **Status:** Proposed for v0.2.0.
  - **Context:** Optional background worker or `systemd --user` unit that monitors network connectivity, pings the configured tracker with exponential backoff, and flushes the SQLite `outbox` queue automatically without requiring manual `alo sync` invocations.

- **RFC-0011 · Multi-Machine State Synchronization Guidelines:**
  - **Status:** Proposed / Documentation.
  - **Context:** Architectural conventions for engineers using multiple physical workstations (e.g., laptop and desktop) to reconcile local SQLite outbox queues cleanly against Plane CE.

---

## 3. Recently Implemented in Core Go

- **Hexagonal Architecture Decoupling:** Segregated domain, application services, canonical ports, and zero adapter-to-adapter leaks.
- **Transactional Multi-Repo Worktrees:** Automatic sibling repository resolution, branch slug sanitization, and rollback upon creation failure (`alo ws`).
- **Pure-Go SQLite Dual-Pool Concurrency:** `ncruces/go-sqlite3` on Wazero with single-writer WAL serialization and parallel query-only readers.
- **Graceful Lifecycle & Signal Handling:** POSIX process group isolation (`Setpgid`) and `signal.NotifyContext` capturing `SIGINT`/`SIGTERM` in Cobra.
- **Strict Machine-to-Machine (M2M) Envelope:** Silent `stderr` under `--json`, structured error categorizations, recovery hints, and POSIX exit codes.
- **Asynchronous Charm TUI Refresh:** Hot-reload (`'r'`) dispatched via `tea.Cmd` goroutine, guaranteeing 60 FPS UI rendering without blocking on I/O.

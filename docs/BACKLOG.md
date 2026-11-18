# Engineering Backlog

Open technical considerations, pending defects, and deferred improvements for Allostasis (`alo`).

---

## 1. Known Defects

- **Attachments regressed in the Go port (high):**
  - **Context:** The legacy Python CLI uploaded attachments (`attachment add|list|delete|replace`, `create --attach`, `get --include attachments`). Reference: tag `archive/python-original`, `legacy/plane-tracker/scripts/plane_cli/attachments.py`.
  - **Current state:** `TrackerService.AddAttachment` is a stub: it never reads the file or calls Plane, and fabricates `SizeBytes: 1024` and a fake `AssetURL`. No CLI command exposes it, `RemoteTrackerClient` has no attachment methods, and [ADR 0025](decisions/0025-json-read-contract.md) lists an `attachment list` view that does not exist.
  - **Plane flow to restore:** `POST …/issues/{id}/issue-attachments/` (name, type, size) → multipart upload to `upload_data.url` with `upload_data.fields` (file last) → `PATCH …/{asset_id}/` with `is_uploaded: true`; on failure `DELETE` the pending asset. Validate before touching Plane: file exists, ≤ 50 MB, `.sql` rejected. Grammar per the CLI guard: `alo ticket attachment add|list|delete|replace`.
- **`ticket update` drops transient remote failures silently (medium):** a network error or 5xx on `UpdateIssueRemote` is ignored and nothing is queued, so the local cache diverges from Plane without notice. Input rejections are already surfaced ([ADR 0031](decisions/0031-mutation-input-errors-fail-closed.md)); updates need an outbox event type.
- **Exit code contract is documented three ways (low):** `AGENTS.md` and `DEVELOPMENT.md` (`3` not found, `4` conflict), `CLI_DESIGN_GUARD.md` (`3` Plane unreachable) and `internal/presentation/errors` (`3` auth, `4` not found, `5` transient). Pick one and align the docs with the code.

---

## 2. Deferred Enhancements & Open RFCs

- **RFC-0014 · Pluggable Multi-Tracker Drivers (Linear / GitHub Issues):**
  - **Status:** Architecture ready; adapter development open.
  - **Context:** The canonical port [`RemoteTrackerClient`](../internal/app/ports.go) in `internal/app/ports.go` abstracts all remote tracking operations. Plane CE is 100% functional. Implementations for Linear GraphQL and GitHub Issues REST can be plugged in without modifying application services or domain logic.

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

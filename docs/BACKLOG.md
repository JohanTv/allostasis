# Engineering Backlog

Open technical considerations, pending defects, and deferred improvements for Allostasis (`alo`).

---

## 1. Known Defects

- **`ticket link list` falls back to the local copy silently (low):** `ListLinks` ignores a remote error and returns cached links without saying so. Give it the same `source`/`note` treatment as `ticket attachment list` ([ADR 0033](decisions/0033-ticket-attachments-require-plane.md)).
- **Stream large attachments (low):** uploads read the whole file (≤ 50 MB) into memory; streaming needs a `Content-Length`-aware multipart writer.
- **`ticket create --attach` / `ticket get --include attachments,links` not restored (decision pending):** the Python CLI had them; the CLI guard prefers composing `create` + `attachment add`. Restore only if wanted.
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

- **`alo sync` solo sincroniza los proyectos de los que el usuario es miembro:** Plane informa `is_member: true` únicamente en los suyos y omite el campo en los públicos del workspace; esos ya no reciben ninguna petición (estados, etiquetas ni tareas: 3 menos por proyecto y por sync, lo que también reduce los 429). La pertenencia es tri-estado (si Plane no la informa en ningún proyecto, nada se excluye), se persiste en `projects.is_member` y `alo projects` la muestra en la columna MIEMBRO. Un proyecto pedido con `-p` o con `--all-projects` se sincroniza igualmente.
- **Un HTTP 403 por proyecto no es un error del sync:** como red de seguridad (la pertenencia puede cambiar entre la lista y la petición), un 403 al listar las tareas de un proyecto de selección implícita se omite y se informa en `skipped_projects`, sin degradar el estado ni llenar `errors`. Sigue siendo error si el proyecto se pide explícito (`-p`), si todos los proyectos dan 403 (credenciales o permisos mal configurados) o si es un 401.
- **Attachments restored end to end (`alo ticket attachment add|list|delete|replace`):** real signed-upload protocol with rollback, validation before the network, source-aware listing ([ADR 0033](decisions/0033-ticket-attachments-require-plane.md)). Replaced a stub that fabricated attachment metadata.
- **Single remote-mutation dispatcher and `update_issue` outbox event:** a typed error classification with default-deny; transient failures are queued and reported (`pending_sync`), rejections and unclassified errors are shown ([ADR 0032](decisions/0032-remote-mutation-dispatch.md)). Fixes `ticket update` diverging from Plane without notice and `link add|delete` faking local records.
- **Labels resolve to UUIDs from the local catalog; unknown labels and states fail closed** ([ADR 0031](decisions/0031-mutation-input-errors-fail-closed.md)).

- **Hexagonal Architecture Decoupling:** Segregated domain, application services, canonical ports, and zero adapter-to-adapter leaks.
- **Transactional Multi-Repo Worktrees:** Automatic sibling repository resolution, branch slug sanitization, and rollback upon creation failure (`alo ws`).
- **Pure-Go SQLite Dual-Pool Concurrency:** `ncruces/go-sqlite3` on Wazero with single-writer WAL serialization and parallel query-only readers.
- **Graceful Lifecycle & Signal Handling:** POSIX process group isolation (`Setpgid`) and `signal.NotifyContext` capturing `SIGINT`/`SIGTERM` in Cobra.
- **Strict Machine-to-Machine (M2M) Envelope:** Silent `stderr` under `--json`, structured error categorizations, recovery hints, and POSIX exit codes.
- **Asynchronous Charm TUI Refresh:** Hot-reload (`'r'`) dispatched via `tea.Cmd` goroutine, guaranteeing 60 FPS UI rendering without blocking on I/O.

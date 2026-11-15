# Development Documentation Index

> Canonical index of Architecture Decision Records (ADRs) and Requests for Comments (RFCs) for Allostasis (`alo`).  
> All documents follow the architecture specification in [ARCHITECTURE.md](ARCHITECTURE.md) and backlog in [BACKLOG.md](BACKLOG.md).

---

## 1. Architecture Decision Records (ADRs)

| ID | Title | Status | Scope in Go Core | Summary |
| :--- | :--- | :---: | :--- | :--- |
| **0007** | [Instance-Agnostic Documents](decisions/0007-instance-agnostic-documents.md) | accepted | Global | Documentation and repositories must not contain hardcoded private IPs, tokens, or instance-specific data. Configuration is resolved via environment variables. |
| **0012** | [Unified Binary Coexistence (CLI + TUI)](decisions/0012-fast-testing-strategy.md) | accepted | `cmd/alo`, `internal/presentation/tui` | Eliminates tool fragmentation (`alo` and `alo-ws` unified under `alo`). Headless machine commands and interactive Charm Bubble Tea cockpits coexist in a single zero-dependency binary. |
| **0015** | [Central Clock & Frozen Clock in Tests](decisions/0015-central-frozen-clock.md) | accepted | `internal/domain/clock` | All time reads pass through a domain `Clock` interface. Testing uses `FrozenClock` for deterministic mathematical validation of Little's Law and Klee Lebesgue measure. |
| **0018** | [Destructive Deletion Safeguards](decisions/0018-destructive-deletion-safeguard.md) | accepted | `cmd/alo/cmd_ticket.go`, `cmd_workspace.go` | `alo ticket delete` requires explicit `--yes` confirmation. `alo ws prune` defaults to non-destructive dry-run simulation and requires `--execute` for actual deletion. |
| **0019** | [Transactional Rollback in Multi-Repo Worktrees](decisions/0019-person-filters-and-assignment-history.md) | accepted | `internal/adapters/gitcli` | If worktree creation fails on repository $N$ of a multi-repo container, the system automatically rolls back and cleans up the preceding $N-1$ created directories. |
| **0025** | [Stable Machine-to-Machine (M2M) JSON Envelope](decisions/0025-json-read-contract.md) | accepted | `internal/presentation/errors` | Strict JSON envelope contract (`ok`, `data`, `error`) for AI agent orchestration. Errors provide `code`, `category`, `hint`, `suggested_command`, and deterministic POSIX exit codes with silent `stderr`. |
| **0028** | [Pure-Go SQLite Dual-Pool with WAL Concurrency](decisions/0028-cqs-delta-sync-watermarks.md) | accepted | `internal/adapters/sqlite` | Compiles without CGO (`ncruces/go-sqlite3` on Wazero). Implements dual-pool WAL concurrency: 1 serialized writer connection (`MaxOpenConns=1`) and parallel reader connections with `query_only=ON`. |

---

## 2. Requests for Comments (RFCs)

| ID | Title | Status | Implementation in Go | Summary |
| :--- | :--- | :---: | :--- | :--- |
| **0002** | [Multi-Repo Worktrees per Ticket](rfcs/0002-worktrees-multi-repo-por-ticket.md) | implemented | `alo ws create`, `alo ws list`, `alo ws audit`, `alo ws prune` | Transactional multi-repository workspace isolation in `.worktrees/<TICKET>-<SLUG>/`, automatic merge detection, and safety `.tar.gz` backups. |
| **0005** | [Personal Activity Agenda & WIP Limits](rfcs/0005-personal-agenda-and-backlog.md) | implemented | `alo agenda` | Local-first personal activity dashboard, overdue detection, Little's Law WIP regulation, and focus task recommendation. |
| **0006** | [Blocker Telemetry & Repository Auto-Resolution](rfcs/0006-external-standby-datasource.md) | implemented | `internal/domain/workspace` | Automatic sister repository resolution in `~/Code/` by convention, and blocker visibility in agenda workflows. |
| **0011** | [Multi-Machine Cache Architecture](rfcs/0011-shared-cache-between-machines.md) | proposed | `internal/adapters/sqlite` | Guidelines and conventions for multi-workstation state synchronization via upstream tracker delta sync rather than distributed databases. |
| **0014** | [Pluggable Multi-Tracker Adapters](rfcs/0014-personal-and-work-contexts.md) | partial | `internal/app/ports.go:RemoteTrackerClient` | Contractual abstraction for interchangeable trackers. Plane CE driver is fully implemented; Linear and GitHub Issues drivers remain open for community extension. |
| **0021** | [Standardized Query & View Read Architecture](rfcs/0021-query-and-view-read-architecture.md) | implemented | `cmd/alo/*`, `--json` | Universal machine output format with silent `stderr` and strict error categorization for LLM agents. |
| **0026** | [Allostatic Load & Klee Interval Measure](rfcs/0026-time-distribution-against-plan.md) | implemented | `alo load`, `internal/domain/ledger` | 1D Lebesgue measure interval union algorithm ($O(N \log N)$) measuring net deep work vs reactive toil without overlapping inflation. |
| **0027** | [Incremental Outbox & Delta Sync](rfcs/0027-incremental-sync-and-granularity.md) | implemented | `alo sync`, `internal/app/tracker.go` | Local-first outbox pattern enqueuing worklogs and issue mutations when offline, with idempotent batch flushing to upstream tracker. |
| **0029** | [Interactive TUI Worktree Navigator](rfcs/0029-tui-worktree-navigator.md) | proposed | Target: v0.2.0 | Interactive Charm Bubble Tea view (`alo ws browse`) for visual inspection, terminal/Zellij launch, and interactive safe pruning. |
| **0030** | [Background Sync Daemon](rfcs/0030-background-sync-daemon.md) | proposed | Target: v0.2.0 | Non-blocking background worker (`alo daemon`) for automatic periodic outbox flushing upon network availability. |

---

## 3. Principles & Operational References

- 🧭 [**Design Principles of Allostasis**](principles.md): Fundamentos ontológicos y principios rectores del producto (P1 a P5, Ley de Little, Medida de Lebesgue de Klee).
- 📚 [**Suite de Referencias y Gobernanza Operativa**](references/README.md):
  - [Protocolo de Living Tickets y Fases del SDLC](references/PROTOCOLO_LIVING_TICKETS_Y_SDLC.md)
  - [Protocolo de Relevo Asíncrono de Tareas (Task Handoff)](references/PROTOCOLO_RELEVO_ASINCRONO_HANDOFF.md)
  - [Matriz de Taxonomía y Protocolo de Tickets Paraguas](references/MATRIZ_TAXONOMIA_Y_TICKETS_PARAGUAS.md)
  - [Estrategia Integral de Gestión TI y Work Items](references/ESTRATEGIA_GESTION_TI_Y_WORKITEMS.md)
  - [Gobernanza y Principios de Diseño del CLI](references/CLI_DESIGN_GUARD.md)


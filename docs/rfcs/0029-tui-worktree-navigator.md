---
id: "0029"
title: Unified Interactive TUI (alo browse)
status: implemented
created: 2026-11-15
updated: 2026-11-22
summary: Unified tabbed Charm Bubble Tea TUI for cockpit metrics, tickets, workspaces, and time logging — accessed via `alo browse` (alias `tui`).
---

# 0029 · Unified Interactive TUI (`alo browse`)

Related: [BACKLOG.md](../BACKLOG.md), [principles.md](../principles.md), RFC [0002](./0002-worktrees-multi-repo-por-ticket.md).

## Context

`alo` nació como una CLI headless para agentes de IA. Todos los comandos (`alo ticket list`, `alo ws list`, `alo agenda`, etc.) funcionan en modo headless con `--json`. La experiencia interactiva para humanos debe accederse explícitamente, no ser el comportamiento por defecto de `alo`.

Antes, el RFC proponía `alo ws browse` enfocado solo en worktrees. Se amplió a una TUI unificada con pestañas navegables.

## Decision

- **`alo` sin argumentos** muestra la ayuda de Cobra (no abre la TUI).
- **`alo browse`** (alias `tui`) abre la interfaz interactiva con pestañas.
- La TUI requiere terminal interactiva; falla con un mensaje claro si se redirige a pipe.

## Proposal

Un comando `alo browse` (alias `tui`) que abre una interfaz Bubble Tea con **pestañas navegables**:

```
🧬 ALLOSTASIS // TUI v0.2.0
[📊 Cockpit]  📋 Tickets  🌳 Workspaces  📅 Log
├─────────────────────────────────────────────────────────┤
│  (Contenido de la pestaña activa)                       │
├─────────────────────────────────────────────────────────┤
│  Tab / Shift+Tab: cambiar pestaña  │  1-4: ir a pestaña │
│  r: refrescar  │  q / esc: salir                        │
└─────────────────────────────────────────────────────────┘
```

**Pestañas:**

1. **📊 Cockpit** — Métricas alostáticas, WIP, Ley de Little, ciclo proyectado, Klee measure. (Reutiliza `CockpitService`)
2. **📋 Tickets** — Tabla interactiva de tareas activas con navegación por flechas. (Reutiliza `CockpitSnapshot.ActiveIssues`)
3. **🌳 Workspaces** — Lista de worktrees con indicadores de salud (✓ Limpio / ⚠ Cambios pendientes). (Reutiliza `CockpitSnapshot.ActiveWorktrees`)
4. **📅 Log** — Referencia a `alo log -i` y lista de tickets activos disponibles para registro rápido.

**Atajos globales:**
- `Tab` / `Shift+Tab`: Cambiar pestaña siguiente/anterior.
- `1`–`4`: Ir directamente a una pestaña.
- `r`: Refrescar datos asíncronamente.
- `q` / `esc`: Salir.

## Arquitectura

- **Nuevo archivo:** `internal/presentation/tui/browse.go` — Modelo Bubble Tea con pestañas (`browseModel`, `Tab`, `RunBrowse`).
- **Nuevo comando:** `cmd/alo/cmd_browse.go` — Cobra command que inicializa los tres servicios y llama a `tui.RunBrowse`.
- **Modificación:** `cmd/alo/root.go` — El `Run` por defecto invoca `cmd.Help()` en lugar de abrir la TUI.
- **Preservado:** `internal/presentation/tui/cockpit.go` — El cockpit original sigue disponible para uso interno (pestaña Cockpit).

## Consequences

- **Dual-mode preservado:** Todos los comandos headless (`alo ticket list --json`, `alo ws list`, etc.) siguen funcionando para CI/agentes sin afectación.
- **Arquitectura hexagonal intacta:** La TUI consume los mismos servicios de aplicación (`CockpitService`, `TrackerService`, `WorkspaceService`). Cero violaciones de aislamiento.
- **Escalable:** Cuando se implemente RFC-0030 (daemon), se puede agregar una pestaña de estado del sync. Cuando venga RFC-0014 (multi-tracker), una pestaña de configuración.
- **`alo` sin argumentos** ya no abre la TUI, respetando el origen de la herramienta como CLI para agentes.

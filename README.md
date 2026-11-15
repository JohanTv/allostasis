# 🧬 Allostasis (`alo`)

<p align="center">
  <strong>The Sovereign Developer Cockpit for Flow, Invisible Work, and Cognitive Load Regulation.</strong>
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License"></a>
  <a href="https://golang.org"><img src="https://img.shields.io/badge/go-1.25+-00ADD8.svg" alt="Go Version"></a>
  <a href="https://charm.land"><img src="https://img.shields.io/badge/TUI-Charm-FF5F87.svg" alt="Charm TUI"></a>
  <a href="#"><img src="https://img.shields.io/badge/architecture-Hexagonal%20%2F%20DDD-green.svg" alt="Architecture"></a>
</p>

---

## 📌 ¿Qué es Allostasis?

En biología y medicina, la **homeostasis** es el intento de mantener el sistema estático e inmóvil. La **alostasis**, en cambio, es la consecución de la **estabilidad mediante el cambio adaptativo continuo** (*stability through change*).

En la ingeniería de software moderna:
- Las interrupciones, el soporte ad-hoc imprevisto, las reuniones y la coordinación invisible constituyen tu **carga alostática** (*allostatic load*).
- Si esta carga no se mide ni se visibiliza, el desarrollador colapsa por sobrecarga cognitiva (*burnout*).
- **`alo`** es tu **cockpit soberano de cabina**:
  1. **Visibiliza el trabajo invisible:** Hace que registrar soporte y coordinación tome un solo comando sin fricción.
  2. **Protege tu autonomía:** Funciona **Local-First** sobre SQLite con CQS. Cero spyware o bossware. El desarrollador tiene control total sobre qué métricas comparte.
  3. **Regula la masa en vuelo:** Aplica la **Ley de Little** para restringir el trabajo en progreso (WIP) y mantener el foco en *Deep Work*.
  4. **Aísla el espacio de trabajo:** Orquesta Git worktrees transaccionales multi-repo para que alternar entre tickets no contamine tu código.

---

## ⚡ Aspectos Técnicos Destacados

* **Dual-Mode por Diseño:**
  - **Headless Mode (`--json`):** Salida estructurada de alta precisión para **agentes autónomos de IA** (Claude Code, Gemini CLI, OpenCode) y pipelines de CI, sin secuencias ANSI.
  - **TUI Reactiva:** Cockpit interactivo para humanos impulsado por el ecosistema **Charm** ([Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lip Gloss](https://github.com/charmbracelet/lipgloss), [Bubbles](https://github.com/charmbracelet/bubbles), [Huh](https://github.com/charmbracelet/huh)).
* **Pure-Go Zero-CGO SQLite:** Motor SQLite oficial compilado a WebAssembly y ejecutado con JIT nativo vía `ncruces/go-sqlite3` y `wazero`. Soporta búsqueda de texto completo **FTS5** (`unicode61`) y modo WAL concurrente sin requerir toolchains de C.
* **Orquestación Git de Máxima Resiliencia:** Auditoría de merges trifásicos en memoria con `git merge-tree --write-tree`, detección de capacidades, aislamiento POSIX (`Setpgid`) y respaldos comprimidos preventivos `.tar.gz`.
* **Arranque Instantáneo:** Cold-start en **< 5ms** para invocaciones transparentes desde prompts de shell y bucles de agentes.

---

## 🚀 Uso Rápido

```bash
# Cockpit Interactivo (TUI con Charm)
alo                        # Abre la estación de cabina interactiva en tiempo real

# Telemetría y Carga Alostática
alo agenda                 # Dashboard de estado del día, focos activos y horas Klee
alo load                   # Balance de trabajo profundo vs soporte invisible (Ley de Little)

# Modo Headless Estricto para Agentes de IA
alo agenda --json          # Payload estructurado sin secuencias ANSI para Claude/Gemini
alo load --json            # Métricas en crudo para toma autónoma de decisiones

# Registro Soberano de Esfuerzo (Local-First)
alo log PROJ-123 45 "Refactorización del dominio hexagonal" --bucket deep_work
alo log OPS-24 15 "Resolución de alerta en monitoreo" --bucket toil_support

# Orquestación de Espacios de Trabajo Multi-Repo (Git Worktrees)
alo ws create PROJ-123 repo-api repo-web --base main
alo ws list                # Ver worktrees activos y estado de modificación
alo ws prune proj-123      # Poda segura con respaldo automático .tar.gz
```

---

## 🏛️ Arquitectura del Repositorio

El proyecto sigue rigurosamente los principios **SOLID**, **Clean Architecture** y **Arquitectura Hexagonal (Ports & Adapters)**:

```text
allostasis/
├── cmd/
│   └── alo/                    # Entrypoint CLI unificado y Composition Root
│       ├── main.go             # Graceful shutdown (SIGINT/SIGTERM) y M2M shielding
│       ├── root.go             # Configuración rootCmd, flags globales e inyección
│       ├── cmd_cockpit.go      # Comandos agenda, load y log
│       ├── cmd_workspace.go    # Subcomandos alo ws (list, create, audit, prune)
│       ├── cmd_sync.go         # Sincronización outbox hacia tracker remoto
│       └── cmd_ticket.go       # Catálogo y gestión de tareas (list, get, create, etc.)
├── internal/
│   ├── domain/                 # 🧠 Núcleo de Dominio puro (cero dependencias externas)
│   │   ├── clock/              # Reloj determinista (Clock, FrozenClock)
│   │   ├── ledger/             # Medida de Klee, AllostaticLoad, Ley de Little
│   │   ├── issue/              # Modelos canónicos WorkItem, State, Label, OutboxRecord
│   │   └── workspace/          # Modelos Container, WorktreeBinding, MergeStatus
│   ├── app/                    # 💼 Capa de Aplicación y Puertos Canónicos
│   │   ├── ports.go            # Interfaces IssueStore, WorklogStore, WorkspaceRunner, RemoteTrackerClient
│   │   ├── cockpit.go          # CockpitService (cálculo de carga alostática y métricas)
│   │   ├── tracker.go          # TrackerService (gestión local-first y sincronización upstream)
│   │   └── workspace.go        # WorkspaceService (orquestación multi-worktree con rollback)
│   ├── adapters/               # ⚙️ Adaptadores de Infraestructura (Outbound)
│   │   ├── sqlite/             # Pure-Go SQLite (Wazero), doble pool concurrente WAL
│   │   ├── gitcli/             # Ejecutor Git con aislamiento PGID y rollback transaccional
│   │   └── tracker_plane/      # Cliente HTTP REST puro desacoplado de base de datos
│   └── presentation/           # 🖥️ Estrategias de Presentación e Interfaz
│       ├── errors/             # Envelope JSON estructurado, categorías y códigos POSIX
│       └── tui/                # Cockpit reactivo no bloqueante (Charm Bubble Tea)
├── docs/                       # 📚 Especificaciones, arquitectura y gobernanza
│   ├── ARCHITECTURE.md         # Especificación técnica canónica y principios SOLID
│   ├── INDEX.md                # Índice maestro de Decisiones (ADRs) y RFCs
│   ├── BACKLOG.md              # Backlog de ingeniería y mejoras diferidas
│   ├── decisions/              # Architecture Decision Records (ADRs 0007 a 0028)
│   └── rfcs/                   # Requests for Comments (RFCs 0002 a 0027)
└── Makefile                    # Compilación, tests con -race e instalación local
```

Para una inmersión detallada en los patrones de diseño y decisiones estructurales, consulte [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), [`docs/INDEX.md`](docs/INDEX.md) y [`docs/BACKLOG.md`](docs/BACKLOG.md).

---

## 📜 Licencia

Distribuido bajo la licencia **Apache 2.0**. Consulte [`LICENSE`](LICENSE) para más detalles.

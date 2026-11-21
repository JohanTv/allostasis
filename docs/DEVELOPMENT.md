# Guía de Desarrollo e Ingeniería Interna: Allostasis (`alo`)

> **Audiencia:** Ingenieros de Core y Agentes Autónomos de IA modificando, extendiendo o auditando el código fuente de `allostasis`.  
> Si tu propósito es únicamente inducción rápida de ciclo de vida o reglas de oro de ejecución, consulta [`AGENTS.md`](../AGENTS.md).

---

## 1. Contexto, Propósito y Arquitectura

**Allostasis (`alo`)** es una cabina de control (*developer cockpit*) soberana, **Local-First**, diseñada para visibilizar el trabajo invisible (soporte, interrupciones, coordinación), regular la carga cognitiva mediante la **Ley de Little** ($L = \lambda \cdot W$) y orquestar Git worktrees multi-repo transaccionales con rollback atómico.

### Topología Hexagonal y Stack Clave:
- **`internal/domain/` (Dominio Puro):** Entidades y reglas canónicas (`WorkItem`, `State`, `Ledger`, `KleeMeasure`, `AllostaticLoad`, `Clock`). **Cero dependencias externas** (solo stdlib de Go: `time`, `math`, `sync`, `sort`, `errors`).
- **`internal/app/` (Aplicación y Puertos):** Contratos segregados en [`internal/app/ports.go`](../internal/app/ports.go) (`IssueStore`, `WorklogStore`, `WorkspaceRunner`, `RemoteTrackerClient`). Servicios orquestadores (`CockpitService`, `TrackerService`, `WorkspaceService`).
- **`internal/adapters/` (Infraestructura Outbound):**
  - `sqlite/`: SQLite sobre WebAssembly (Wazero JIT) vía `ncruces/go-sqlite3`. Modo WAL concurrente y FTS5 (`unicode61`).
  - `gitcli/`: Subprocesos Git con aislamiento POSIX (`Setpgid`), merge trifásico en memoria y rollback transaccional.
  - `tracker_plane/`: Cliente HTTP REST desacoplado hacia Plane CE.
- **`internal/presentation/` (Presentación Inbound):**
  - `errors/`: Blindaje Machine-to-Machine (`ResponseEnvelope` JSON formal, códigos POSIX).
  - `tui/`: Interfaz interactiva basada en Charm ([Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lip Gloss](https://github.com/charmbracelet/lipgloss)).

Para especificaciones canónicas de diseño, consulta [`docs/ARCHITECTURE.md`](ARCHITECTURE.md), [`docs/BACKLOG.md`](BACKLOG.md) y [`docs/principles.md`](principles.md).

---

## 2. Comandos de Ciclo de Vida (`Makefile`)

Todo desarrollo o refactorización debe operarse mediante los objetivos estándar del `Makefile`:

```bash
make build       # Compila bin/alo con CGO_ENABLED=0 y flags de release
make test        # Ejecuta la suite de pruebas completa con race detector (-race)
make docs        # Regenera docs/INDEX.md a partir de los metadatos de RFCs y ADRs
make docs-check  # Linter de documentación en CI: valida enlaces relativos y consistencia
make install     # Compila e instala alo en ~/.local/bin/alo (con symlinks allostasis y allo)
make clean       # Limpia binarios generados en bin/
```

> [!IMPORTANT]
> **Prohibición de bypass:** NUNCA compiles omitiendo `CGO_ENABLED=0`. NUNCA omitas el flag `-race` al ejecutar pruebas (`go test -race ./...`).

---

## 3. Invariantes Profundos de Implementación

Al implementar nuevas características o refactorizar código en `allostasis`, debes preservar los siguientes invariantes arquitectónicos:

### 3.1. Núcleo de Dominio y Reloj Determinista
- **Reloj Determinista:** NUNCA uses `time.Now()` directamente en la lógica de dominio o servicios de aplicación. Utiliza la abstracción canónica [`clock.Clock`](../internal/domain/clock/clock.go):
  - `RealClock` en producción.
  - `FrozenClock` en pruebas unitarias y de simulación temporal.
- **Modelos Puros:** Las entidades de dominio no conocen bases de datos, protocolos de red ni frameworks CLI.

### 3.2. Capa de Aplicación y Puertos Canónicos
- **Inversión de Dependencias (DIP):** Los servicios dependen únicamente de las interfaces en `ports.go`.
- **Zero Adapter-to-Adapter Leaks:** Queda terminantemente prohibido que un adaptador importe o llame a otro adaptador (por ejemplo, `tracker_plane` no conoce SQLite; `gitcli` no conoce la base de datos).

### 3.3. Adaptador de Persistencia SQLite (`internal/adapters/sqlite/`)
- **Dual-Pool WAL:**
  - Conexión de escritura única: `SetMaxOpenConns(1)` para serializar mutaciones y prevenir bloqueos `SQLITE_BUSY`.
  - Conexiones de lectura concurrentes: `SetMaxOpenConns(runtime.NumCPU() * 2)` con `_pragma=query_only(ON)` para lecturas en paralelo sin contención.
- **FTS5:** Tablas virtuales con tokenizador `unicode61` para búsqueda de texto completo rápida y tolerante.

### 3.4. Adaptador Git CLI (`internal/adapters/gitcli/`)
- **Aislamiento POSIX (`Setpgid`):** Todos los subprocesos de Git deben lanzarse en su propio grupo de procesos para asegurar la terminación inmediata ante cancelación por `context.Context` (e.g. `SIGINT`/`SIGTERM`), evitando procesos zombies en el host.
- **Rollback Transaccional:** En operaciones multi-repo (`alo ws create`), si la creación falla en el repositorio $N$, el adaptador debe destruir y limpiar de forma atómica los worktrees creados en los repositorios $1 \dots N-1$.
- **Seguridad Defensiva:**
  - *Anti Flag-Injection:* Rechazar argumentos o referencias que comiencen con `-`.
  - *Anti Path-Traversal:* Rechazar rutas que contengan `../` fuera del contenedor `.worktrees/`.

### 3.5. Presentación y Blindaje Machine-to-Machine
- **Silencio Absoluto en `stderr` bajo `--json`:** Si se activa `--json`, `stderr` debe permanecer en **0 bytes**. Ningún log, aviso o traza puede contaminar `stderr`.
- **ResponseEnvelope Canónico:** Toda respuesta debe emitirse serializada en JSON por `stdout` conforme al esquema canónico:
  ```json
  {
    "ok": false,
    "error": {
      "code": "WORKTREE_MERGE_CONFLICT",
      "category": "conflict",
      "message": "existen conflictos con la rama base main",
      "hint": "Resuelve los conflictos en el contenedor antes de podar",
      "recoverable": true
    }
  }
  ```
- **Códigos POSIX Estándar (Canónicos):** `0` (éxito), `1` (error interno/no clasificado), `2` (CLI usage / datos rechazados), `3` (auth/credenciales), `4` (not found), `5` (upstream transitorio/timeout), `70` (software crash / pánico interceptado). Ver especificación formal en [`docs/references/CLI_DESIGN_GUARD.md`](references/CLI_DESIGN_GUARD.md) y [`internal/presentation/errors/envelope.go`](../internal/presentation/errors/envelope.go).

---

## 4. Ciclo de Vida Documental (`cmd/docgen`)

El repositorio mantiene su documentación técnica viva y sincronizada con el código mediante el motor documental en Go ([`cmd/docgen`](../cmd/docgen/main.go)).

### 4.1. Anotación Rápida de Bugs y Pendientes (`docs/BACKLOG.md`)
- Si durante una sesión detectas un defecto, una limitación técnica o una mejora diferida que no vas a resolver en el turno actual:
  - Anótalo en [`docs/BACKLOG.md`](BACKLOG.md) bajo la sección correspondiente (`## 1. Known Defects` o `## 2. Deferred Enhancements & Open RFCs`).
  - **Fricción cero:** No requiere metadatos YAML pesados ni IDs formales para notas rápidas.
  - Si un bug ya fue resuelto, muévelo a `## 3. Recently Implemented in Core Go`.

### 4.2. Propuesta de Cambios y Contratos (`cmd/docgen`)
- Para cambios arquitectónicos significativos, nuevos adaptadores o contratos formales, crea un documento técnico usando `docgen`:
  ```bash
  # Crear un Architecture Decision Record (ADR)
  go run ./cmd/docgen --new decision <slug-kebab-case> --title "Título descriptivo"

  # Crear una Request for Comments (RFC)
  go run ./cmd/docgen --new rfc <slug-kebab-case> --title "Título descriptivo"
  ```
- `docgen` asigna automáticamente el siguiente ID secuencial de 4 dígitos (ej: `0031`), genera la plantilla con frontmatter YAML normalizado y valida la sintaxis.
- Tras redactar el documento, sincroniza el índice maestro:
  ```bash
  make docs
  ```

### 4.3. Inmutabilidad Histórica
- **NUNCA borres un RFC o ADR** que ya haya sido mergeado o implementado.
- Si una decisión queda obsoleta o un RFC es superado por otro diseño:
  - Edita el frontmatter: cambia `status: superseded` o `status: deprecated`.
  - Agrega una nota indicando el nuevo documento que lo reemplaza.
  - Ejecuta `make docs` y `make docs-check` para actualizar [`docs/INDEX.md`](INDEX.md).

---

## 5. Checklist de Verificación antes de Finalizar un Turno

Antes de entregar tu respuesta o marcar una tarea como completada en este repositorio, verifica de manera determinista:

1. [ ] **`make test`**: La suite completa pasa sin fallos y con cero condiciones de carrera (`-race`).
2. [ ] **`make docs-check`**: Cero errores de enlaces rotos, desincronizaciones de IDs o inconsistencias en `docs/INDEX.md`.
3. [ ] **`make build`**: El binario compila limpiamente en `bin/alo` con `CGO_ENABLED=0`.
4. [ ] **`git status -s`**: El working tree contiene únicamente los cambios intencionales, sin residuos temporales, logs o volcados SQLite huérfanos.

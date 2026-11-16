# Directrices de Desarrollo para Agentes de IA: Allostasis (`alo`)

> **Audiencia:** Agentes Autónomos de IA (Claude Code, Gemini CLI, OpenCode, Cursor, Antigravity) e Ingenieros de Core.  
> **Propósito:** Guía de inducción rápida (*onboarding*), arquitectura del sistema, invariantes inquebrantables de código y ciclo de vida de desarrollo.

---

## 1. Contexto, Propósito y Arquitectura

**Allostasis (`alo`)** es una cabina de control (*developer cockpit*) soberana, **Local-First**, diseñada para visibilizar el trabajo invisible (soporte, interrupciones, coordinación), regular la carga cognitiva mediante la **Ley de Little** ($L = \lambda \cdot W$) y orquestar Git worktrees multi-repo transaccionales con rollback atómico.

### Stack y Decisiones Clave:
- **Arquitectura Hexagonal (Ports & Adapters) + DDD:** Separación estricta de responsabilidades. El dominio es agnóstico a la infraestructura; la aplicación define contratos (puertos); los adaptadores implementan I/O.
- **Pure-Go CGO_ENABLED=0:** Compilación 100% estática sin dependencias de toolchains C.
- **SQLite Embebido sobre WebAssembly (Wazero JIT):** Motor oficial SQLite vía `ncruces/go-sqlite3`. Soporta FTS5 (`unicode61`) y modo WAL.
- **Dual-Pool WAL Concurrency:** 1 escritor serializado (`SetMaxOpenConns(1)`) para evitar `SQLITE_BUSY`, y un pool de lectores concurrentes escalado por CPU con `PRAGMA query_only=ON`.
- **Twelve-Factor CLI:** Arranque instantáneo (<5ms), configuración transparente, procesos sin estado fuera de la base local y soporte dual:
  - **TUI Reactiva Humana:** Interfaz interactiva basada en Charm ([Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lip Gloss](https://github.com/charmbracelet/lipgloss)).
  - **Headless M2M para Agentes (`--json`):** Salida estructurada de alta fidelidad, determinista, libre de secuencias ANSI y con silenciamiento total de `stderr`.

---

## 2. Comandos de Ciclo de Vida

Todo agente que desarrolle o modifique código en este repositorio debe operar mediante los objetivos estándar del `Makefile`:

```bash
make build       # Compila el binario bin/alo con CGO_ENABLED=0 y flags de release
make test        # Ejecuta la suite de pruebas completa con race detector (-race)
make docs        # Regenera docs/INDEX.md a partir de los metadatos de RFCs y ADRs
make docs-check  # Linter de documentación en CI: valida enlaces relativos y consistencia
make install     # Compila e instala alo en ~/.local/bin/alo (con symlinks allostasis y allo)
make clean       # Limpia binarios generados en bin/
```

> [!IMPORTANT]
> **Prohibición de bypass:** NUNCA ejecutes `go build` sin `CGO_ENABLED=0`. NUNCA omitas el flag `-race` al ejecutar pruebas de concurrencia o repositorios (`go test -race ./...`).

---

## 3. Invariantes Inquebrantables de Código (Non-Negotiable Invariants)

Al implementar nuevas características o refactorizar código en `allostasis`, debes preservar los siguientes invariantes arquitectónicos:

### 3.1. Núcleo de Dominio (`internal/domain/`)
- **Aislamiento Absoluto:** **Cero dependencias** hacia SQL, HTTP, Git, Cobra, Charm o sistemas de archivos. Solo se permite el uso de la biblioteca estándar de Go (`time`, `math`, `sync`, `sort`, `errors`).
- **Reloj Determinista:** NUNCA uses `time.Now()` directamente en la lógica de dominio o servicios de aplicación. Utiliza la abstracción canónica [`clock.Clock`](internal/domain/clock/clock.go):
  - `RealClock` en producción.
  - `FrozenClock` en pruebas unitarias y de simulación temporal.
- **Modelos Puros:** Entidades canónicas ricas (`WorkItem`, `State`, `Container`, `Ledger`, `KleeMeasure`, `AllostaticLoad`).

### 3.2. Capa de Aplicación y Puertos (`internal/app/`)
- **Puertos Canónicos Segregados (ISP):** Definidos exclusivamente en [`internal/app/ports.go`](internal/app/ports.go):
  - `IssueStore`: Persistencia y consulta local-first de tickets, estados y cola outbox.
  - `WorklogStore`: Almacenamiento del ledger de tiempo soberano y cálculo de Klee.
  - `WorkspaceRunner`: Orquestación de worktrees y subprocesos Git.
  - `RemoteTrackerClient`: Abstracción HTTP hacia el tracker remoto (Plane CE, Linear, etc.).
- **Inversión de Dependencias (DIP):** Los servicios (`CockpitService`, `TrackerService`, `WorkspaceService`) dependen únicamente de estas interfaces.
- **Zero Adapter-to-Adapter Leaks:** Queda terminantemente prohibido que un adaptador importe o llame a otro adaptador (por ejemplo, `tracker_plane` no conoce SQLite; `gitcli` no conoce la base de datos).

### 3.3. Presentación y Blindaje Machine-to-Machine (`internal/presentation/errors/`)
- **Silencio Absoluto en `stderr` bajo `--json`:** Cuando se activa el modo `--json`, `stderr` debe permanecer en **0 bytes**. Ningún log, aviso o traza puede contaminar `stderr`.
- **ResponseEnvelope Canónico:** Toda respuesta (exitosa o con error) debe emitirse por `stdout` serializada en JSON bajo el esquema formal:
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
- **Códigos de Salida POSIX Estándar:**
  - `0`: Operación exitosa.
  - `1`: Error de dominio / ejecución interna.
  - `2`: Uso incorrecto del CLI / flag inválido (`UsageError`).
  - `3`: Recurso no encontrado.
  - `4`: Conflicto de estado o prerrequisito no cumplido.

### 3.4. Adaptador de Persistencia SQLite (`internal/adapters/sqlite/`)
- **Dual-Pool WAL:**
  - Conexión de escritura única: `SetMaxOpenConns(1)` para serializar mutaciones y prevenir bloqueos `SQLITE_BUSY`.
  - Conexiones de lectura concurrentes: `SetMaxOpenConns(runtime.NumCPU() * 2)` con `_pragma=query_only(ON)` para lecturas en paralelo sin contención.
- **FTS5:** Tablas virtuales con tokenizador `unicode61` para búsqueda de texto completo rápida y tolerante.

### 3.5. Adaptador Git CLI (`internal/adapters/gitcli/`)
- **Aislamiento POSIX (`Setpgid`):** Todos los subprocesos de Git deben lanzarse en su propio grupo de procesos para asegurar la terminación inmediata ante cancelación por `context.Context` (e.g. `SIGINT`/`SIGTERM`), evitando procesos zombies en el host.
- **Rollback Transaccional:** En operaciones multi-repo (`alo ws create`), si la creación falla en el repositorio $N$, el adaptador debe destruir y limpiar de forma atómica los worktrees creados en los repositorios $1 \dots N-1$.
- **Seguridad Defensiva:**
  - *Anti Flag-Injection:* Rechazar argumentos o referencias que comiencen con `-`.
  - *Anti Path-Traversal:* Rechazar rutas que contengan `../` fuera del contenedor `.worktrees/`.

---

## 4. Ciclo de Vida Documental (`docs/`)

El repositorio mantiene su documentación técnica viva y sincronizada con el código mediante el motor documental en Go ([`cmd/docgen`](cmd/docgen/main.go)).

### 4.1. Anotación Rápida de Bugs y Pendientes (`docs/BACKLOG.md`)
- Si durante una sesión detectas un defecto, una limitación técnica o una mejora diferida que no vas a resolver en el turno actual:
  - Anótalo en [`docs/BACKLOG.md`](docs/BACKLOG.md) bajo la sección correspondiente (`## 1. Known Defects` o `## 2. Deferred Enhancements & Open RFCs`).
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
  - Ejecuta `make docs` y `make docs-check` para actualizar [`docs/INDEX.md`](docs/INDEX.md).

---

## 5. Checklist de Verificación antes de Finalizar un Turno

Antes de entregar tu respuesta o marcar una tarea como completada en este repositorio, verifica de manera determinista:

1. [ ] **`make test`**: La suite completa pasa sin fallos y con cero condiciones de carrera (`-race`).
2. [ ] **`make docs-check`**: Cero errores de enlaces rotos, desincronizaciones de IDs o inconsistencias en `docs/INDEX.md`.
3. [ ] **`make build`**: El binario compila limpiamente en `bin/alo` con `CGO_ENABLED=0`.
4. [ ] **`git status -s`**: El working tree contiene únicamente los cambios intencionales, sin residuos temporales, logs o volcados SQLite huérfanos.

# Directrices de Agentes: Allostasis (`alo`)

> **Referencias:** Arquitectura hexagonal, diseño SOLID y protocolo M2M en `go-cli-tui`. Adaptadores, persistencia SQLite y Git en [`DEVELOPMENT.md`](docs/DEVELOPMENT.md).

---

## 🛡️ 1. Invariantes Críticas

1. **Pure-Go & Concurrencia:** `CGO_ENABLED=0` estricto y `-race` obligatorio en pruebas.
2. **Reloj Determinista:** Prohibido `time.Now()` en dominio o aplicación. Usa exclusivamente [`clock.Clock`](internal/domain/clock/clock.go).
3. **Aislamiento Hexagonal:** `internal/domain/` tiene cero dependencias externas. Cero imports directos entre adaptadores.
4. **Persistencia & Tests:** Resuelve bases de datos exclusivamente vía `internal/config.ResolveDatabasePath`. Prohibido tocar la DB de producción (`~/.cache/...`); tests en `t.TempDir()`.
5. **Protocolo M2M (`--json`):** `stderr` en 0 bytes; `stdout` emite `ResponseEnvelope`. La matriz normativa de códigos POSIX (`0, 1, 2, 3, 4, 5, 70`) reside en [`CLI_DESIGN_GUARD.md`](docs/references/CLI_DESIGN_GUARD.md).

---

## ✅ 2. Validación y Checklist Pre-Entrega

Valida todo cambio exclusivamente mediante el `Makefile`:
1. [ ] **`make test`** pasa al 100% con `-race` (cero data races).
2. [ ] **`make docs-check`** pasa sin enlaces rotos ni inconsistencias de índice (`make docs` sincroniza índice tras RFCs/ADRs).
3. [ ] **`make build`** compila limpiamente en `bin/alo` con `CGO_ENABLED=0`.
4. [ ] **`git status -s`** libre de DBs temporales, binarios no rastreados o logs (cambios de código en progreso no requieren commit).

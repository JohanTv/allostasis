# Directrices de Agentes: Allostasis (`alo`)

> **Propósito:** Inducción técnica y salvaguardas operativas de desarrollo para agentes de IA en este repositorio.  
> **Referencias:** Para arquitectura hexagonal, diseño SOLID y protocolo M2M en Go, consulta la habilidad `go-cli-tui`. Si vas a modificar adaptadores, persistencia SQLite o subprocesos Git, consulta [`DEVELOPMENT.md`](docs/DEVELOPMENT.md).

---

## ⚡ 1. Comandos de Ciclo de Vida y Validación

Valida todo cambio exclusivamente mediante el `Makefile`:

```bash
make test        # Suite unitaria e integración con race detector (-race obligatorio)
make build       # Compilación estática con CGO_ENABLED=0 en bin/alo
make docs-check  # Linter de consistencia documental y enlaces relativos
make docs        # Sincroniza docs/INDEX.md tras actualizar RFCs/ADRs
```

---

## 🛡️ 2. Invariantes Críticas

1. **Pure-Go & Concurrencia:** `CGO_ENABLED=0` estricto y `-race` obligatorio en pruebas.
2. **Reloj Determinista:** Prohibido `time.Now()` en dominio o aplicación. Usa exclusivamente [`clock.Clock`](internal/domain/clock/clock.go) (`RealClock` en prod, `FrozenClock` en tests).
3. **Aislamiento Hexagonal:** `internal/domain/` tiene cero dependencias externas. Cero imports directos entre adaptadores.
4. **Persistencia & Tests:** Resuelve bases de datos exclusivamente vía `internal/config.ResolveDatabasePath`. Prohibido tocar la DB de producción (`~/.cache/...`); tests corren aislados en `t.TempDir()`.
5. **Protocolo M2M (`--json`):** `stderr` en 0 bytes; `stdout` emite `ResponseEnvelope`. La matriz normativa de códigos POSIX (`0, 1, 2, 3, 4, 5, 70`) reside en [`CLI_DESIGN_GUARD.md`](docs/references/CLI_DESIGN_GUARD.md).

---

## ✅ 3. Checklist Pre-Entrega

Antes de finalizar tu turno o entregar código en este repositorio:
1. [ ] **`make test`** pasa al 100% con `-race` (cero data races).
2. [ ] **`make docs-check`** pasa sin enlaces rotos ni inconsistencias de índice.
3. [ ] **`make build`** compila limpiamente en `bin/alo` con `CGO_ENABLED=0`.
4. [ ] **`git status -s`** limpio de volcados SQLite, artefactos de test o logs temporales.

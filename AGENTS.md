# Directrices de Agentes: Allostasis (`alo`)

> **Propósito:** Inducción técnica y salvaguardas operativas de desarrollo para agentes de IA en este repositorio.  
> **Patrones y Arquitectura:** Para principios generales de arquitectura hexagonal, diseño SOLID y protocolo M2M en Go, consulta la habilidad [`go-cli-tui`](file:///home/johantv/.agents/skills/go-cli-tui/SKILL.md). Si vas a modificar adaptadores, persistencia SQLite o subprocesos Git, consulta [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md).

---

## ⚡ 1. Comandos Canónicos de Ciclo de Vida

Todo agente que opere en este repositorio debe validar su trabajo mediante el `Makefile`:

```bash
make test        # Suite de pruebas unitarias y de integración con race detector (-race)
make build       # Compilación estática con CGO_ENABLED=0 en bin/alo
make docs-check  # Linter de consistencia de arquitectura y validación de enlaces relativos
make docs        # Sincroniza docs/INDEX.md tras crear o actualizar RFCs/ADRs
```

---

## 🛡️ 2. Invariantes Específicas del Repositorio

1. **Pure-Go & Concurrencia:** Compilación estricta con `CGO_ENABLED=0`. Prohibido omitir el flag `-race` al ejecutar pruebas (`go test -race ./...`).
2. **Reloj Determinista:** Prohibido usar `time.Now()` en dominio o aplicación. Usa exclusivamente la abstracción canónica [`clock.Clock`](file:///home/johantv/Code/allostasis/internal/domain/clock/clock.go) (`RealClock` en prod, `FrozenClock` en tests).
3. **Aislamiento Hexagonal:** `internal/domain/` tiene **cero dependencias externas**. Un adaptador nunca importa directamente a otro adaptador.
4. **Persistencia y Aislamiento de Tests:**
   - La resolución de base de datos se realiza exclusivamente mediante `internal/config.ResolveDatabasePath`.
   - Prohibido que las pruebas apunten o muten la base de datos de producción (`~/.cache/allostasis/allostasis.db`). Todo harness corre aislado en `t.TempDir()`.
5. **Protocolo Machine-to-Machine (`--json`):**
   - Con `--json`, `stderr` permanece en **0 bytes** y `stdout` emite JSON bajo el contrato `ResponseEnvelope` (ver detalles en [`go-cli-tui`](file:///home/johantv/.agents/skills/go-cli-tui/SKILL.md)).

---

## ✅ 3. Checklist Pre-Entrega

Antes de finalizar tu turno o entregar código en este repositorio:
1. [ ] **`make test`** pasa al 100% con `-race` (cero data races).
2. [ ] **`make docs-check`** pasa sin enlaces rotos ni inconsistencias de índice.
3. [ ] **`make build`** compila limpiamente en `bin/alo` con `CGO_ENABLED=0`.
4. [ ] **`git status -s`** limpio de volcados SQLite, artefactos de test o logs temporales.

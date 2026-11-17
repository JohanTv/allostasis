# 🔄 Protocolo de Living Tickets y Fases del SDLC
## Guía de Gestión Evolutiva de Tareas en Incertidumbre para Plane CE v1.2.3

**Ámbito:** Repositorios de trabajo (`Omnify`, `Operaciones`, servicios satélite)  
**Propósito:** Eliminar la falsa certeza y el sub-registro de horas permitiendo que los tickets nazcan con información parcial de diagnóstico y maduren hasta su cierre verificado bajo el estándar de **Pirámide Invertida** y **Declaración Explícita de Evidencias**.

---

## 1. El Ciclo de 4 Fases (SDLC Canónico)

Todo requerimiento técnico en la organización se clasifica en una de las 4 fases:

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│ 1. [SPIKE]      │ ──> │ 2. [RFC]        │ ──> │ 3. [FEAT/FIX]   │ ──> │ 4. [QA]         │
│ Diagnóstico/PoC │     │ Diseño/Schema   │     │ Construcción    │     │ Validación/Test │
└─────────────────┘     └─────────────────┘     └─────────────────┘     └─────────────────┘
```

1. `[SPIKE]` **Auditoría Forense & Diagnóstico:** Aislamiento de causa raíz, investigación técnica, análisis de datos en réplica y PoC descartable.
2. `[RFC]` **Análisis de Implementación & Diseño:** Modelado de schema de base de datos, contratos de API, especificaciones de arquitectura y diagramas de flujo.
3. `[FEAT/FIX/INFRA]` **Construcción & DevOps:** Implementación de código en frontend/backend, migraciones de base de datos, Docker, Caddy, endpoints.
4. `[QA]` **Testing & Validación Transaccional:** Pruebas unitarias/integración, validación en réplica con `ROLLBACK`, recálculo de balances y dry-runs.

---

## 2. La Regla de los 15 Minutos (Umbral de Creación)

Para evitar la creación de micro-tickets basura que destruyan las métricas del sprint y saturen el tablero Kanban:

* **Tareas < 15 minutos (Toil / Soporte Rápido / Dailies):**  
  * *Acción:* **NO crear ticket en Omnify**.
  * *Destino:* Absorber en el ticket paraguas semanal de `OPS`:
    `Soporte y Mantenimiento Ad-Hoc - W{XX}` o `Sincronización de Equipo y Acuerdos - W{XX}`.
  * *Registro:* `alo log OPS-XX <DURACION_MINUTOS> "Detalle del soporte" --bucket toil_support`.
* **Tareas > 15 minutos (Trabajo Estructurado):**  
  * *Acción:* Crear ticket formal en Plane **al minuto 15**.
  * *Destino:* Proyecto respectivo (`OMNIFY` por defecto, o `OPS`).
  * *Estado Inicial:* Nace bajo la **Plantilla de Incertidumbre (Fase 1)**.

---

## 3. Arquitectura del Living Ticket: El Principio de la Pirámide Invertida

Toda descripción de ticket en Plane CE (creada vía `alo ticket create --desc "..."` o interfaz web) debe estructurarse bajo la **Pirámide Invertida** (≤ 250 palabras). La información crítica debe concentrarse al inicio para permitir que cualquier desarrollador o agente entre en contexto en los primeros segundos mediante `alo ticket get`.

```
┌────────────────────────────────────────────────────────────────────────┐
│               ANATOMÍA DE LA PIRÁMIDE INVERTIDA EN PLANE              │
├────────────────────────────────────────────────────────────────────────┤
│ 1. TÍTULO SDLC     │ [{FASE}] {Acción concisa en español}              │
│                    │ ej. [FIX] Corrección de menú flotante de chips... │
├────────────────────┼───────────────────────────────────────────────────┤
│ 2. RESUMEN LEAD    │ 1 sola oración que resume la intención completa.  │
│    (Nivel 0)       │ Suficiente para previews y listados rápidos.      │
├────────────────────┼───────────────────────────────────────────────────┤
│ 3. CONTEXTO/CAUSA  │ Viñetas con IDENTIFICADORES TÉCNICOS REALES:      │
│    (Nivel 1)       │ • Componentes exactos: CampaignChipFloatingMenu   │
│                    │ • Defectos estructurales: overflow-hidden, portal │
│                    │ • Referencias Git/API: PR #265 en dev, endpoints  │
├────────────────────┼───────────────────────────────────────────────────┤
│ 4. SOLUCIÓN /      │ • FEAT/FIX: Primitivas (DropdownMenu, portal).    │
│    OPCIONES        │ • SPIKE: Opciones a evaluar (Opción A vs B vs C). │
│                    │   NUNCA inventar soluciones prematuras en spikes. │
├────────────────────┼───────────────────────────────────────────────────┤
│ 5. EVIDENCIAS /    │ DECLARACIÓN OBLIGATORIA DE RECURSOS:              │
│    ADJUNTOS        │ • Capturas: screenshots/2026-11-16_19-41-06.png    │
│                    │ • Figma / Sentry / Logs: URLs o Comentario #1     │
├────────────────────┼───────────────────────────────────────────────────┤
│ 6. CRITERIO DE     │ • FEAT/FIX: DoD binario [ ] con tests y build.    │
│    CIERRE          │ • SPIKE/RFC: Checklist de investigación/veredicto.│
└────────────────────┴───────────────────────────────────────────────────┘
```

> [!IMPORTANT]
> **Declaración Obligatoria de Evidencias y Adjuntos:**  
> Si un ticket tiene capturas de pantalla, grabaciones, diseños de Figma o trazas de Sentry, **DEBEN listarse explícitamente en la descripción**. Los agentes autónomos operan en entornos headless/CLI y no pueden "adivinar" si existen archivos visuales si no están declarados en el texto.

---

## 4. Plantillas Canónicas por Fase

### FASE 1: Estado Inicial (Triage & Incertidumbre — [SPIKE] o Bug No Aislado)
**Cuándo se usa:** Al iniciar un incidente o spike donde aún no se conoce la causa raíz exacta.

```markdown
**Resumen:** [Qué falla exactamente y bajo qué condiciones de frontera].

**Contexto & Síntoma:**
- Módulo / Componente: `nombre_modulo` (ej. Webhook Detonador SMS)
- Anomalía: [1 párrafo conciso con códigos HTTP, latencias o discrepancias clave].

**Evidencias & Adjuntos:**
- Captura UI: `screenshots/2026-11-16_19-41-06.png` (o URL en Plane)
- Logs / Payload: Ver comentario [1] (toda traza extensa va en comentario, nunca en la descripción)

**Hipótesis Preliminares:**
- [ ] Hipótesis A: [Posible causa 1]
- [ ] Hipótesis B: [Posible causa 2]

**Plan de Investigación (Spike Checklist):**
- [ ] Reproducir con payload idéntico en réplica de BD o test de integración.
- [ ] Aislar la condición de carrera o línea exacta en logs.
```

*(Toda traza extensa, snippet de código o volcado SQL se publica de inmediato como primer comentario con `alo ticket comment add <TICKET> "<TRAZA>"`, nunca dentro de la descripción).*

---

### FASE 2: Estado Intermedio (Causa Raíz Aislada ➔ Diseño y Solución — [RFC] o [FEAT/FIX])
**Cuándo se usa:** En cuanto se identifica el origen exacto del problema. Se actualiza la descripción en Plane mediante `alo ticket update <TICKET> --desc "..."`:

```markdown
**Resumen:** [Qué falla y qué se implementará para resolverlo].

**Contexto & Causa Raíz:**
- Componente afectado: `CampaignChipFloatingMenu` (overflow-hidden en acordeón)
- Referencia upstream: PR #265 en origin/dev

**Evidencias & Adjuntos:**
- Captura UI verificada: `screenshots/2026-11-16_19-41-06.png`
- Especificación API / Swagger: `docs/api_sms_campaign_manual.md`

**Solución Propuesta:**
1. [Primitiva o refactor]: Migrar menú a DropdownMenu/Popover con portal al body y colisiones.
2. [Ajuste de contrato]: Actualizar selector con line-clamp-2 y botón de inspección Eye.

**Criterios de Aceptación Técnicos (DoD):**
- [ ] Menú flotante no se corta en bordes de acordeón.
- [ ] Selector muestra 2 líneas por ítem y botón de inspección Eye.
- [ ] Entorno de trabajo multi-repo configurado vía `alo ws create`.
- [ ] Pruebas unitarias y validación `npm run check` / `go test -race` pasando sin errores.
```

---

### FASE 3: Estado Final (Construcción, QA y Cierre)
**Cuándo se usa:** El código está mergeado, validado en réplica o staging, y listo para dar el ticket por concluido.

```markdown
**Resumen:** [Resumen de la solución completada y verificada].

**Criterios de Aceptación Técnicos (DoD):**
- [x] Parche defensivo implementado en el componente/controlador respectivo.
- [x] Pruebas transaccionales en réplica ejecutadas satisfactoriamente (Dry-Run exitoso).
- [x] Rama Git creada bajo el patrón `fix/OMNIFY-263-dropdown-portal-fix`.
- [x] npm run check / tests automáticos pasando al 100%.

**Entregables Verificables:**
- Archivo modificado: `src/components/campaigns/CampaignChipFloatingMenu.vue`
- Pull Request: PR #278 (esperando revisión técnica)
```

Al marcar la Fase 3, se actualiza el estado y se registra la telemetría final:
```bash
alo log OMNIFY-263 95 "Cierre: migración a Popover con portal y validación en navegadores" --bucket deep_work
alo ticket update OMNIFY-263 --state "Completado"
```

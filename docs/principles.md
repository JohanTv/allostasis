# Design Principles of Allostasis (`alo`)

These principles guide what gets built and how. RFCs and decisions cite them by reference. All content in this repository is instance-agnostic: see [ADR 0007](decisions/0007-instance-agnostic-documents.md).

---

## P1 · Visibility of Invisible Work
Support, unplanned production emergencies, meetings, ad-hoc requests, and coordination constitute real engineering work. If untracked, they disappear from visibility, falsely suggesting inactivity or low performance on formal tickets while draining cognitive stamina. The tool must make logging friction-free (one single CLI command: `alo log <TICKET> <MINUTOS>`) and elevate this work alongside core feature development.

## P2 · Autonomy & Anti-Bossware
Engineers retain full sovereign ownership of what is logged, when, and what gets published. Nothing is submitted automatically without user confirmation, nothing is guessed on their behalf, and time tracking measures timestamps and intervals, never keystrokes, active window tracking, or private content.

## P3 · Data for the Engineer, Not Just Management
Every metric captured must first serve the engineer doing the work: understanding active WIP, overdue commitments, cognitive fatigue, and weekly time distribution. Upward reporting is an explicit derived projection strictly controlled by the engineer.

## P4 · Cognitive Load Reduction & Allostasis
The number of open tasks an engineer holds in memory is a measurable quantity. Work-in-progress (WIP) visibility, ticket age, and concise session worklogs exist to reduce context switching and cognitive strain. In living systems, allostasis defines *stability through change*; `alo` regulates cognitive stability by enforcing flow limits.

## P5 · Expectation Management & Empirical Forecasts
Commitments, possibilities, and reports must align honestly. A deadline that passes silently or a dashboard claiming metrics without telemetry breaks trust. Forecasting relies on empirical distributions (cycle time percentiles via Little's Law), never wishful estimates.

---

## Core Technical Principles

- **Limited Work in Progress (Little's Law):** $L = \lambda \cdot W$. Restricting active WIP shortens cycle times and reduces cognitive burden deterministically.
- **Interval Merging (Klee's Algorithm):** 1D Lebesgue measure interval union ($O(N \log N)$) measuring net deep work vs reactive toil without overlapping inflation.
- **Local-First & CQS:** Read operations run locally against SQLite in $< 5\text{ms}$; mutations persist locally with an outbox queue and reconcile via `alo sync`.
- **Single Source of Truth:** Upstream state belongs in the remote tracker (Plane); local state belongs in SQLite WAL; document lifecycle belongs in document front-matter.
- **Do Not Claim What Was Not Measured:** If telemetry is missing or partial, reports must state this clearly or remain silent.

Related: [RFC 0005](rfcs/0005-personal-agenda-and-backlog.md), [RFC 0026](rfcs/0026-time-distribution-against-plan.md).

---
id: "0029"
title: Interactive TUI Worktree Navigator (alo ws browse)
status: proposed
created: 2026-11-15
summary: Interactive Charm Bubble Tea view for visual container inspection, terminal or Zellij launch, and interactive safe pruning.
---

# 0029 · Interactive TUI Worktree Navigator (`alo ws browse`)

Related: [BACKLOG.md](../BACKLOG.md), [principles.md](../principles.md), RFC [0002](./0002-worktrees-multi-repo-por-ticket.md).

## Context

Managing multiple Git worktree containers across repositories currently relies on headless commands:
- `alo ws list` to inspect active containers.
- `alo ws audit` to check merge status.
- `alo ws prune` to clean merged or abandoned containers.

Engineers switching between several active tasks benefit from an interactive, keyboard-driven cockpit that visually surfaces container health and facilitates rapid context transitions.

## Proposal

Expand the Charm Bubble Tea presentation layer with `alo ws browse`:
1. **Interactive Container List:**
   - Visual container table with health color indicators (green: clean/active; yellow: stale/abandoned; blue: merged/ready to prune).
   - Real-time Git branch status and uncommitted changes detection.
2. **Keybindings & Actions:**
   - `<Enter>`: Opens a terminal or Zellij tab/session attached to the selected container.
   - `<p>`: Triggers an interactive safe prune modal with automatic `.tar.gz` backup confirmation.
   - `<c>`: Opens an interactive creation form for new multi-repo worktrees.
   - `<r>`: Asynchronously refreshes container status without blocking UI rendering.
   - `<q>` / `Esc`: Exits the interactive navigator.

## Consequences

- Leverages existing `WorkspaceService` and `WorkspaceRunner` ports without introducing database leaks.
- Preserves dual-mode operation: headless commands remain scriptable for CI/AI agents, while `browse` enhances developer ergonomics.

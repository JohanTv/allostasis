package main

import (
	"context"
	"fmt"
	"os"

	"github.com/JohanTv/allostasis/internal/presentation/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var browseJSONFlag bool

func newBrowseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "browse",
		Aliases: []string{"tui"},
		Short:   "Abre la interfaz interactiva (TUI) de Allostasis",
		Long: `Abre la interfaz interactiva de Allostasis con navegación por pestañas:
  📊 Cockpit  — Métricas alostáticas, WIP, Ley de Little y ciclo proyectado.
  📋 Tickets  — Lista interactiva de tareas con búsqueda y filtrado.
  🌳 Workspaces — Navegador de worktrees con indicadores de salud.
  📅 Log     — Formulario interactivo de registro de tiempo.

Atajos globales:
  Tab / Shift+Tab  Cambiar pestaña
  r                Refrescar datos
  q / esc          Salir`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if browseJSONFlag {
				return fmt.Errorf("el comando 'browse' no es compatible con --json (requiere terminal interactiva)")
			}

			if !term.IsTerminal(int(os.Stdout.Fd())) {
				return fmt.Errorf("la TUI requiere una terminal interactiva. Usa 'alo agenda' para salida headless")
			}

			cockpitSvc, cockpitStore, _, err := initCockpitService()
			if err != nil {
				return fmt.Errorf("inicialización fallida: %w", err)
			}
			defer cockpitStore.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			snap, err := cockpitSvc.GetSnapshot(ctx, ".worktrees")
			if err != nil {
				return fmt.Errorf("no se pudo obtener snapshot: %w", err)
			}

			if err := tui.RunBrowse(cockpitSvc, snap); err != nil {
				return fmt.Errorf("error en interfaz TUI: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&browseJSONFlag, "json", false, "salida JSON (incompatible con browse)")
	return cmd
}

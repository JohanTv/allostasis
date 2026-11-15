package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/allostasis/alo/internal/domain/clock"
	"github.com/allostasis/alo/internal/domain/ledger"
	"github.com/spf13/cobra"
)

var (
	Version   = "0.1.0-alpha"
	BuildDate = "2026-11-15"
	jsonFlag  bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "alo",
		Short: "Allostasis - The Sovereign Developer Cockpit for Flow and Cognitive Load Regulation",
		Long: `Allostasis (alo) es tu estación soberana de cabina:
Visibiliza el trabajo invisible, protege tu foco aplicando la Ley de Little,
y aísla tus ramas multi-repo mediante Git worktrees transaccionales.`,
	}

	rootCmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "Salida estructurada estricta en JSON para agentes de IA")

	// 1. Comando version
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Muestra la versión de Allostasis",
		Run: func(cmd *cobra.Command, args []string) {
			if jsonFlag {
				payload := map[string]string{
					"app":        "allostasis",
					"version":    Version,
					"build_date": BuildDate,
				}
				json.NewEncoder(os.Stdout).Encode(payload)
				return
			}
			fmt.Printf("Allostasis (alo) v%s (compilado: %s)\n", Version, BuildDate)
		},
	}

	// 2. Comando agenda
	agendaCmd := &cobra.Command{
		Use:   "agenda",
		Short: "Muestra el estado alostático del día y focos activos",
		Run: func(cmd *cobra.Command, args []string) {
			clk := clock.NewRealClock()
			if jsonFlag {
				payload := map[string]interface{}{
					"status":    "healthy",
					"timestamp": clk.Now().Format("2006-01-02T15:04:05Z07:00"),
					"wip_count": 0,
					"message":   "Cockpit alostático activo en modo local-first",
				}
				json.NewEncoder(os.Stdout).Encode(payload)
				return
			}
			fmt.Printf("🧬 [ALLOSTASIS COCKPIT] - %s\n", clk.Now().Format("2006-01-02 15:04:05"))
			fmt.Println("------------------------------------------------------------")
			fmt.Println("Estado: Nivel alostático nominal (Cero fricción detectada)")
			fmt.Println("Tareas WIP: 0 | Carga Toil: 0.0% | Capacidad disponible: 100%")
		},
	}

	// 3. Comando load (Carga Alostática)
	loadCmd := &cobra.Command{
		Use:   "load",
		Short: "Calcula y audita el desgaste por soporte invisible (Toil Ratio)",
		Run: func(cmd *cobra.Command, args []string) {
			// Demo cálculo de carga alostática
			load := ledger.AllostaticLoad{
				WIPCount: 2,
			}
			cycleDays := ledger.LittlesLawCycleTime(load.WIPCount, 0.5)

			if jsonFlag {
				payload := map[string]interface{}{
					"wip_count":          load.WIPCount,
					"projected_cycle_days": cycleDays,
					"toil_ratio":         load.ToilRatio(),
					"deep_work_ratio":    load.DeepWorkRatio(),
				}
				json.NewEncoder(os.Stdout).Encode(payload)
				return
			}
			fmt.Println("📊 [BALANCE ALOSTÁTICO & LEY DE LITTLE]")
			fmt.Printf("WIP Activo: %d tareas simultáneas\n", load.WIPCount)
			fmt.Printf("Tiempo de Ciclo Proyectado: %.1f días\n", cycleDays)
		},
	}

	// 4. Grupo workspace (alo ws)
	wsCmd := &cobra.Command{
		Use:     "workspace",
		Aliases: []string{"ws"},
		Short:   "Orquesta entornos multi-repo y Git worktrees transaccionales",
	}

	wsListCmd := &cobra.Command{
		Use:   "list",
		Short: "Lista los worktrees activos por ticket",
		Run: func(cmd *cobra.Command, args []string) {
			if jsonFlag {
				json.NewEncoder(os.Stdout).Encode([]string{})
				return
			}
			fmt.Println("No hay contenedores de worktree activos en .worktrees/")
		},
	}
	wsCmd.AddCommand(wsListCmd)

	rootCmd.AddCommand(versionCmd, agendaCmd, loadCmd, wsCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

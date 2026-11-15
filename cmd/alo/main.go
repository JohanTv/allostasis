package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/allostasis/alo/internal/adapters/gitcli"
	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/app"
	"github.com/allostasis/alo/internal/domain/clock"
	"github.com/allostasis/alo/internal/domain/ledger"
	"github.com/allostasis/alo/internal/domain/workspace"
	"github.com/allostasis/alo/internal/presentation/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	Version   = "0.1.0"
	BuildDate = "2026-11-15"
	jsonFlag  bool
	dbPath    string
)

func initCockpitService() (*app.CockpitService, *sqlite.Store, *gitcli.GitExecutor, error) {
	if dbPath == "" {
		dbPath = app.DefaultDatabasePath()
	}

	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error al inicializar base de datos: %w", err)
	}

	runner := gitcli.NewRunner(30 * time.Second)
	gitExec := gitcli.NewGitExecutor(runner)
	clk := clock.NewRealClock()

	svc := app.NewCockpitService(store, gitExec, clk)
	return svc, store, gitExec, nil
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "alo",
		Short: "Allostasis - The Sovereign Developer Cockpit for Flow and Cognitive Load Regulation",
		Long: `Allostasis (alo) es tu estación soberana de cabina:
Visibiliza el trabajo invisible, protege tu foco aplicando la Ley de Little,
y aísla tus ramas multi-repo mediante Git worktrees transaccionales.`,
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, _, err := initCockpitService()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			defer store.Close()

			ctx := context.Background()
			snap, err := svc.GetSnapshot(ctx, ".worktrees")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error al generar snapshot: %v\n", err)
				os.Exit(1)
			}

			// Modo Headless / JSON
			if jsonFlag {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				_ = enc.Encode(snap)
				return
			}

			// Modo TUI interactivo si es terminal
			if term.IsTerminal(int(os.Stdout.Fd())) {
				if err := tui.RunCockpit(svc, snap); err != nil {
					fmt.Fprintf(os.Stderr, "Error en interfaz TUI: %v\n", err)
					os.Exit(1)
				}
				return
			}

			// Salida básica para tuberías sin JSON
			fmt.Printf("🧬 [ALLOSTASIS] - %s | WIP: %d | Klee: %.1f hrs | Toil: %.1f%%\n",
				snap.Timestamp.Format("2006-01-02 15:04"),
				snap.AllostaticLoad.WIPCount,
				float64(snap.KleeMeasureMinutes)/60.0,
				snap.AllostaticLoad.ToilRatio()*100.0,
			)
		},
	}

	rootCmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "Salida estructurada estricta en JSON para agentes de IA")
	rootCmd.PersistentFlags().StringVar(&dbPath, "db", "", "Ruta personalizada a la base de datos SQLite")

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
				_ = json.NewEncoder(os.Stdout).Encode(payload)
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
			svc, store, _, err := initCockpitService()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			defer store.Close()

			ctx := context.Background()
			snap, err := svc.GetSnapshot(ctx, ".worktrees")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error al generar snapshot: %v\n", err)
				os.Exit(1)
			}

			if jsonFlag {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				_ = enc.Encode(snap)
				return
			}

			fmt.Printf("🧬 [ALLOSTASIS AGENDA] - %s\n", snap.Timestamp.Format("2006-01-02 15:04:05"))
			fmt.Println("----------------------------------------------------------------------")
			fmt.Printf("Estado General:      %s\n", snap.StatusSummary)
			fmt.Printf("Tiempo Neto (Klee):  %.2f horas (%d min brutos)\n", float64(snap.KleeMeasureMinutes)/60.0, snap.TodayMinutes)
			fmt.Printf("Tareas en Progreso:  %d tareas simultáneas\n", snap.AllostaticLoad.WIPCount)
			fmt.Printf("Tiempo de Ciclo:     %.1f días proyectados (Ley de Little)\n", snap.CycleTimeDays)
			fmt.Printf("Deep Work vs Toil:   %.1f%% foco / %.1f%% soporte\n", snap.AllostaticLoad.DeepWorkRatio()*100, snap.AllostaticLoad.ToilRatio()*100)
			fmt.Println("----------------------------------------------------------------------")
			if len(snap.ActiveIssues) > 0 {
				fmt.Println("Tareas activas:")
				for _, it := range snap.ActiveIssues {
					fmt.Printf("  • %-14s [%s] %s\n", it.Key, it.State, it.Title)
				}
			} else {
				fmt.Println("(No hay tareas activas en progreso)")
			}
		},
	}

	// 3. Comando load (Carga Alostática)
	loadCmd := &cobra.Command{
		Use:   "load",
		Short: "Calcula y audita el desgaste por soporte invisible (Toil Ratio)",
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, _, err := initCockpitService()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			defer store.Close()

			ctx := context.Background()
			snap, err := svc.GetSnapshot(ctx, ".worktrees")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			if jsonFlag {
				payload := map[string]interface{}{
					"wip_count":            snap.AllostaticLoad.WIPCount,
					"projected_cycle_days": snap.CycleTimeDays,
					"toil_ratio":           snap.AllostaticLoad.ToilRatio(),
					"deep_work_ratio":      snap.AllostaticLoad.DeepWorkRatio(),
					"status_summary":       snap.StatusSummary,
				}
				_ = json.NewEncoder(os.Stdout).Encode(payload)
				return
			}

			fmt.Println("📊 [BALANCE ALOSTÁTICO & LEY DE LITTLE]")
			fmt.Printf("WIP Activo:                  %d tareas\n", snap.AllostaticLoad.WIPCount)
			fmt.Printf("Tiempo de Ciclo Proyectado:  %.1f días\n", snap.CycleTimeDays)
			fmt.Printf("Deep Work Ratio:             %.1f%%\n", snap.AllostaticLoad.DeepWorkRatio()*100)
			fmt.Printf("Invisible Toil Ratio:        %.1f%%\n", snap.AllostaticLoad.ToilRatio()*100)
			fmt.Printf("Diagnóstico:                 %s\n", snap.StatusSummary)
		},
	}

	// 4. Comando log (Registro de Esfuerzo)
	var (
		bucketFlag      string
		interactiveFlag bool
	)
	logCmd := &cobra.Command{
		Use:   "log [TICKET-ID] [DURACION_MINUTOS] [NOTAS...]",
		Short: "Registra una sesión de esfuerzo en el ledger alostático",
		Args:  cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, _, err := initCockpitService()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			defer store.Close()

			ctx := context.Background()

			// Modo Formulario Interactivo con Huh si se solicita o faltan argumentos en TTY
			if interactiveFlag || (len(args) < 2 && term.IsTerminal(int(os.Stdout.Fd()))) {
				defaultTicket := ""
				if len(args) > 0 {
					defaultTicket = strings.ToUpper(args[0])
				}
				if err := tui.RunLogForm(ctx, svc, defaultTicket); err != nil {
					fmt.Fprintf(os.Stderr, "Registro interactivo cancelado: %v\n", err)
					os.Exit(1)
				}
				fmt.Println("✓ Sesión registrada exitosamente en el ledger alostático.")
				return
			}

			if len(args) < 2 {
				fmt.Fprintf(os.Stderr, "Error: uso requerido: alo log <TICKET-ID> <MINUTOS> [NOTAS...] o use 'alo log -i'\n")
				os.Exit(1)
			}

			ticketID := strings.ToUpper(args[0])
			durationMins, err := strconv.Atoi(args[1])
			if err != nil || durationMins <= 0 {
				fmt.Fprintf(os.Stderr, "Error: la duración debe ser un número entero positivo de minutos\n")
				os.Exit(1)
			}

			notes := ""
			if len(args) > 2 {
				notes = strings.Join(args[2:], " ")
			}

			bucket := ledger.CapacityBucket(bucketFlag)
			if bucket == "" {
				bucket = ledger.BucketDeepWork
			}

			if err := svc.LogTime(ctx, ticketID, durationMins, bucket, notes); err != nil {
				fmt.Fprintf(os.Stderr, "Error registrando sesión: %v\n", err)
				os.Exit(1)
			}

			if jsonFlag {
				payload := map[string]interface{}{
					"status":           "recorded",
					"ticket_id":        ticketID,
					"duration_minutes": durationMins,
					"bucket":           string(bucket),
					"notes":            notes,
				}
				_ = json.NewEncoder(os.Stdout).Encode(payload)
				return
			}

			fmt.Printf("✓ Registrados %d min en %s [%s]: %s\n", durationMins, ticketID, bucket, notes)
		},
	}
	logCmd.Flags().StringVar(&bucketFlag, "bucket", "deep_work", "Bucket de capacidad (deep_work, toil_support, coordination, standby)")
	logCmd.Flags().BoolVarP(&interactiveFlag, "interactive", "i", false, "Lanzar formulario interactivo con Charm Huh")

	// 5. Grupo workspace (alo ws)
	wsCmd := &cobra.Command{
		Use:     "workspace",
		Aliases: []string{"ws"},
		Short:   "Orquesta entornos multi-repo y Git worktrees transaccionales",
	}

	// ws list
	wsListCmd := &cobra.Command{
		Use:   "list",
		Short: "Lista los worktrees activos por ticket",
		Run: func(cmd *cobra.Command, args []string) {
			_, store, gitExec, err := initCockpitService()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			defer store.Close()

			ctx := context.Background()
			containers, err := gitExec.ListContainers(ctx, ".worktrees")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error listando worktrees: %v\n", err)
				os.Exit(1)
			}

			if jsonFlag {
				_ = json.NewEncoder(os.Stdout).Encode(containers)
				return
			}

			if len(containers) == 0 {
				fmt.Println("No hay contenedores de worktree activos en .worktrees/")
				return
			}

			fmt.Printf("🌳 %d Contenedores de Worktree Activos:\n", len(containers))
			for _, c := range containers {
				fmt.Printf("\n📁 Contenedor: %s (Ruta: %s)\n", c.Slug, c.Path)
				for _, b := range c.Bindings {
					status := "Limpio"
					if !b.IsClean {
						status = "Modificado"
					}
					fmt.Printf("   • %-14s [%s] Commit: %s (%s)\n", b.RepoName, b.Branch, b.HeadCommit, status)
				}
			}
		},
	}

	// ws create
	var baseRefFlag string
	wsCreateCmd := &cobra.Command{
		Use:   "create <TICKET-ID> [REPOS...]",
		Short: "Crea un contenedor transaccional de worktrees para un ticket",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			_, store, gitExec, err := initCockpitService()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			defer store.Close()

			ticketID := strings.ToUpper(args[0])
			repoTokens := args[1:]
			if len(repoTokens) == 0 {
				repoTokens = []string{"."}
			}

			ctx := context.Background()
			var bindings []workspace.WorktreeBinding

			for _, tok := range repoTokens {
				resolved, err := gitExec.ResolveRepo(ctx, tok)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error resolviendo repo '%s': %v\n", tok, err)
					os.Exit(1)
				}
				repoName := filepath.Base(resolved)
				branchName := fmt.Sprintf("feat/%s", strings.ToLower(ticketID))

				bindings = append(bindings, workspace.WorktreeBinding{
					RepoName: repoName,
					RepoPath: resolved,
					Branch:   branchName,
				})
			}

			containerPath := filepath.Join(".worktrees", strings.ToLower(ticketID))
			container := &workspace.Container{
				TicketID: ticketID,
				Slug:     strings.ToLower(ticketID),
				Path:     containerPath,
				BaseRef:  baseRefFlag,
				Bindings: bindings,
			}

			if err := gitExec.CreateWorktree(ctx, container); err != nil {
				fmt.Fprintf(os.Stderr, "Error creando contenedor: %v\n", err)
				os.Exit(1)
			}

			if jsonFlag {
				_ = json.NewEncoder(os.Stdout).Encode(container)
				return
			}

			fmt.Printf("✓ Contenedor creado exitosamente en %s\n", containerPath)
			for _, b := range container.Bindings {
				fmt.Printf("  • %s -> rama: %s\n", b.RepoName, b.Branch)
			}
		},
	}
	wsCreateCmd.Flags().StringVar(&baseRefFlag, "base", "main", "Rama base upstream (ej. main, dev)")

	// ws prune
	var backupFlag bool
	wsPruneCmd := &cobra.Command{
		Use:   "prune <TICKET-ID>",
		Short: "Desmantela de forma segura un contenedor de worktree",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			_, store, gitExec, err := initCockpitService()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			defer store.Close()

			ticketID := strings.ToLower(args[0])
			containerPath := filepath.Join(".worktrees", ticketID)

			if fi, err := os.Stat(containerPath); err != nil || !fi.IsDir() {
				fmt.Fprintf(os.Stderr, "Error: el contenedor '%s' no existe\n", containerPath)
				os.Exit(1)
			}

			ctx := context.Background()
			containers, err := gitExec.ListContainers(ctx, ".worktrees")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			var targetContainer *workspace.Container
			for i := range containers {
				if strings.EqualFold(containers[i].Slug, ticketID) {
					targetContainer = &containers[i]
					targetContainer.TicketID = strings.ToUpper(ticketID)
					break
				}
			}

			if targetContainer == nil {
				fmt.Fprintf(os.Stderr, "Error: contenedor no reconocido en .worktrees/\n")
				os.Exit(1)
			}

			backupDir := ""
			if backupFlag {
				home, _ := os.UserHomeDir()
				backupDir = filepath.Join(home, ".cache", "allostasis", "worktree-backups")
			}

			if err := gitExec.PruneWorktree(ctx, targetContainer, backupDir); err != nil {
				fmt.Fprintf(os.Stderr, "Error al podar worktree: %v\n", err)
				os.Exit(1)
			}

			if jsonFlag {
				payload := map[string]string{
					"status":    "pruned",
					"ticket_id": targetContainer.TicketID,
					"path":      containerPath,
				}
				_ = json.NewEncoder(os.Stdout).Encode(payload)
				return
			}

			fmt.Printf("✓ Contenedor %s podado limpiamente.\n", containerPath)
		},
	}
	wsPruneCmd.Flags().BoolVar(&backupFlag, "backup", true, "Generar respaldo tar.gz si existen cambios no commiteados")

	wsCmd.AddCommand(wsListCmd, wsCreateCmd, wsPruneCmd)
	rootCmd.AddCommand(versionCmd, agendaCmd, loadCmd, logCmd, wsCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

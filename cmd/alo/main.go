package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/allostasis/alo/internal/adapters/gitcli"
	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/adapters/tracker_plane"
	"github.com/allostasis/alo/internal/app"
	"github.com/allostasis/alo/internal/domain/clock"
	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/domain/ledger"
	"github.com/allostasis/alo/internal/domain/workspace"
	"github.com/allostasis/alo/internal/presentation/errors"
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

func initTrackerService() (*app.TrackerService, *sqlite.Store, error) {
	if dbPath == "" {
		dbPath = app.DefaultDatabasePath()
	}

	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("error al inicializar base de datos: %w", err)
	}

	planeCfg := tracker_plane.LoadConfigFromEnv()
	var client *tracker_plane.Client
	if planeCfg.APIKey != "" {
		client = tracker_plane.NewClient(planeCfg)
	}

	svc := app.NewTrackerService(store, client)
	return svc, store, nil
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
				errors.EmitSuccess(snap, true)
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
				errors.EmitSuccess(payload, true)
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
				errors.EmitSuccess(snap, true)
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
				errors.EmitSuccess(payload, true)
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
				errors.EmitSuccess(payload, true)
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
				if containers == nil {
					containers = make([]workspace.Container, 0)
				}
				errors.EmitSuccess(containers, true)
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
				errors.EmitSuccess(container, true)
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
				errors.EmitSuccess(payload, true)
				return
			}

			fmt.Printf("✓ Contenedor %s podado limpiamente.\n", containerPath)
		},
	}
	wsPruneCmd.Flags().BoolVar(&backupFlag, "backup", true, "Generar respaldo tar.gz si existen cambios no commiteados")

	// 6. Comando sync (Sincronización con Plane CE)
	var limitFlag int
	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Sincroniza eventos diferidos (outbox) y comentarios con Plane CE",
		Run: func(cmd *cobra.Command, args []string) {
			_, store, _, err := initCockpitService()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la ruta a la base de datos", jsonFlag)
			}
			defer store.Close()

			cfg := tracker_plane.LoadConfigFromEnv()
			client := tracker_plane.NewClient(cfg)

			ctx := context.Background()
			report, err := client.FlushOutbox(ctx, store.DB(), limitFlag)
			if err != nil {
				errors.EmitError(err, "SYNC_FAILED", "Verifica la conectividad con Plane CE o PLANE_API_KEY", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(report, true)
				return
			}

			fmt.Printf("🔄 [SINCRONIZACIÓN PLANE CE] - Estado: %s\n", report.Status)
			fmt.Printf("   • Comentarios publicados: %d\n", report.SentComments)
			fmt.Printf("   • Fallidos / Reintentos:  %d\n", report.FailedComments)
			for _, e := range report.Errors {
				fmt.Printf("   ⚠ %s\n", e)
			}
		},
	}
	syncCmd.Flags().IntVar(&limitFlag, "limit", 20, "Límite máximo de eventos outbox a procesar por lote")

	// 7. Comando ticket (Gestión soberana e inspección de tareas)
	ticketCmd := &cobra.Command{
		Use:     "ticket",
		Aliases: []string{"issue", "issues"},
		Short:   "Consulta, búsqueda tolerante y gestión de tareas",
	}

	var ticketProjectFlag string
	var ticketStateFlag string
	var ticketLimitFlag int
	var ticketTextFlag string
	var ticketAllProjectsFlag, ticketBriefFlag, ticketMineFlag, ticketOpenFlag bool

	ticketListCmd := &cobra.Command{
		Use:   "list",
		Short: "Lista tareas registradas en la base de datos local",
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la ruta a la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			var items []issue.WorkItem

			targetProject := ticketProjectFlag
			if ticketAllProjectsFlag {
				targetProject = ""
			}

			if ticketTextFlag != "" {
				items, err = svc.Search(ctx, ticketTextFlag, targetProject, ticketLimitFlag)
			} else {
				filter := issue.IssueFilter{
					ProjectKey: targetProject,
					State:      ticketStateFlag,
					Limit:      ticketLimitFlag,
				}
				if ticketOpenFlag {
					filter.StateGroup = "started"
				}
				items, err = svc.List(ctx, filter)
			}
			if err != nil {
				errors.EmitError(err, "QUERY_FAILED", "Error al consultar tareas", jsonFlag)
			}

			if jsonFlag {
				payload := map[string]interface{}{
					"count":   len(items),
					"results": items,
					"schema":  "plane-tracker/1",
					"view":    "list",
				}
				errors.EmitSuccess(payload, true)
				return
			}

			if len(items) == 0 {
				fmt.Println("No se encontraron tareas.")
				return
			}

			fmt.Printf("%-14s %-12s %-8s %s\n", "TICKET", "ESTADO", "PRIO", "TÍTULO")
			fmt.Println(strings.Repeat("─", 80))
			for _, it := range items {
				st := it.StateName
				if st == "" {
					st = string(it.State)
				}
				fmt.Printf("%-14s %-12s %-8s %s\n", it.Key, st, it.Priority, it.Title)
			}
		},
	}
	ticketListCmd.Flags().StringVarP(&ticketProjectFlag, "project", "p", "", "Filtrar por proyecto (ej. OMNIFY, OPS)")
	ticketListCmd.Flags().StringVarP(&ticketStateFlag, "state", "s", "", "Filtrar por estado (ej. Progreso, Backlog)")
	ticketListCmd.Flags().IntVarP(&ticketLimitFlag, "limit", "l", 30, "Límite de resultados")
	ticketListCmd.Flags().StringVar(&ticketTextFlag, "text", "", "Búsqueda por texto libre")
	ticketListCmd.Flags().BoolVar(&ticketAllProjectsFlag, "all-projects", false, "Buscar en todos los proyectos")
	ticketListCmd.Flags().BoolVar(&ticketBriefFlag, "brief", false, "Salida concisa")
	ticketListCmd.Flags().BoolVar(&ticketMineFlag, "mine", false, "Mis tareas")
	ticketListCmd.Flags().BoolVar(&ticketOpenFlag, "open", false, "Solo tareas abiertas")

	ticketSearchCmd := &cobra.Command{
		Use:   "search <término>",
		Short: "Búsqueda tolerante en títulos, etiquetas y descripciones",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la ruta a la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			items, err := svc.Search(ctx, args[0], ticketProjectFlag, ticketLimitFlag)
			if err != nil {
				errors.EmitError(err, "SEARCH_FAILED", "Error al buscar tareas", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(items, true)
				return
			}

			if len(items) == 0 {
				fmt.Printf("No se encontraron tareas con el término '%s'.\n", args[0])
				return
			}

			fmt.Printf("Resultados para '%s':\n", args[0])
			fmt.Printf("%-14s %-12s %-8s %s\n", "TICKET", "ESTADO", "PRIO", "TÍTULO")
			fmt.Println(strings.Repeat("─", 80))
			for _, it := range items {
				st := it.StateName
				if st == "" {
					st = string(it.State)
				}
				fmt.Printf("%-14s %-12s %-8s %s\n", it.Key, st, it.Priority, it.Title)
			}
		},
	}
	ticketSearchCmd.Flags().StringVarP(&ticketProjectFlag, "project", "p", "", "Filtrar por proyecto")
	ticketSearchCmd.Flags().IntVarP(&ticketLimitFlag, "limit", "l", 20, "Límite de resultados")

	ticketGetCmd := &cobra.Command{
		Use:   "get <TICKET-ID>",
		Short: "Obtiene el detalle completo de una tarea",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la ruta a la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			item, err := svc.Get(ctx, args[0])
			if err != nil {
				errors.EmitError(err, "NOT_FOUND", fmt.Sprintf("Verifica la clave de la tarea: %s", args[0]), jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(item, true)
				return
			}

			st := item.StateName
			if st == "" {
				st = string(item.State)
			}
			fmt.Printf("📌 %s: %s\n", item.Key, item.Title)
			fmt.Printf("   • Estado    : %s\n", st)
			fmt.Printf("   • Prioridad : %s\n", item.Priority)
			if item.Assignee != "" {
				fmt.Printf("   • Asignado  : %s\n", item.Assignee)
			}
			if item.Description != "" {
				fmt.Printf("\n📄 Descripción:\n   %s\n", strings.ReplaceAll(item.Description, "\n", "\n   "))
			}
		},
	}

	var createProjectFlag, createPriorityFlag, createStateFlag, createDescFlag string
	ticketCreateCmd := &cobra.Command{
		Use:   "create <título>",
		Short: "Crea una nueva tarea (en Plane CE o local si está offline)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la ruta a la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			created, err := svc.Create(ctx, issue.CreateIssueInput{
				ProjectKey:  createProjectFlag,
				Title:       args[0],
				Description: createDescFlag,
				Priority:    createPriorityFlag,
				StateName:   createStateFlag,
			})
			if err != nil {
				errors.EmitError(err, "CREATE_FAILED", "No se pudo crear la tarea", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(created, true)
				return
			}

			fmt.Printf("✓ Tarea creada exitosamente: %s (%s)\n", created.Key, created.Title)
		},
	}
	ticketCreateCmd.Flags().StringVarP(&createProjectFlag, "project", "p", "OMNIFY", "Proyecto destino (ej. OMNIFY, OPS)")
	ticketCreateCmd.Flags().StringVar(&createPriorityFlag, "priority", "medium", "Prioridad (low, medium, high, urgent)")
	ticketCreateCmd.Flags().StringVar(&createStateFlag, "state", "Pendiente", "Estado inicial")
	ticketCreateCmd.Flags().StringVar(&createDescFlag, "desc", "", "Descripción de la tarea")

	var updateStateFlag, updatePriorityFlag, updateTitleFlag string
	ticketUpdateCmd := &cobra.Command{
		Use:   "update <TICKET-ID>",
		Short: "Actualiza el estado, prioridad o título de una tarea",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la ruta a la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			input := issue.UpdateIssueInput{}
			if updateStateFlag != "" {
				input.StateName = &updateStateFlag
			}
			if updatePriorityFlag != "" {
				input.Priority = &updatePriorityFlag
			}
			if updateTitleFlag != "" {
				input.Title = &updateTitleFlag
			}

			updated, err := svc.Update(ctx, args[0], input)
			if err != nil {
				errors.EmitError(err, "UPDATE_FAILED", "No se pudo actualizar la tarea", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(updated, true)
				return
			}

			fmt.Printf("✓ Tarea %s actualizada: Estado=%s Prioridad=%s\n", updated.Key, updated.StateName, updated.Priority)
		},
	}
	ticketUpdateCmd.Flags().StringVar(&updateStateFlag, "state", "", "Nuevo estado")
	ticketUpdateCmd.Flags().StringVar(&updatePriorityFlag, "priority", "", "Nueva prioridad")
	ticketUpdateCmd.Flags().StringVar(&updateTitleFlag, "title", "", "Nuevo título")

	var syncAllProjectsFlag bool
	ticketSyncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Descarga y actualiza proyectos, estados y tareas desde Plane CE",
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la ruta a la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			count, err := svc.Sync(ctx, ticketProjectFlag, syncAllProjectsFlag)
			if err != nil {
				errors.EmitError(err, "SYNC_FAILED", "Verifica la conectividad con Plane CE", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(map[string]interface{}{"synced_issues": count}, true)
				return
			}

			fmt.Printf("🔄 Sincronización completada: %d tareas actualizadas en SQLite.\n", count)
		},
	}
	ticketSyncCmd.Flags().StringVarP(&ticketProjectFlag, "project", "p", "", "Proyecto específico a sincronizar")
	ticketSyncCmd.Flags().BoolVar(&syncAllProjectsFlag, "all-projects", false, "Sincronizar todos los proyectos")

	ticketWhoamiCmd := &cobra.Command{
		Use:   "whoami",
		Short: "Verifica la conectividad y credenciales con Plane CE",
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la ruta a la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			user, err := svc.Whoami(ctx)
			if err != nil {
				errors.EmitError(err, "AUTH_FAILED", "Verifica PLANE_API_KEY y PLANE_BASE_URL", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(user, true)
				return
			}

			fmt.Println("✅ Conexión con Plane CE verificada:")
			fmt.Printf("   • Usuario : %s (%s %s)\n", user.Username, user.FirstName, user.LastName)
			fmt.Printf("   • Email   : %s\n", user.Email)
			fmt.Printf("   • ID      : %s\n", user.ID)
		},
	}

	ticketCmd.AddCommand(ticketListCmd, ticketSearchCmd, ticketGetCmd, ticketCreateCmd, ticketUpdateCmd, ticketSyncCmd, ticketWhoamiCmd)
	wsCmd.AddCommand(wsListCmd, wsCreateCmd, wsPruneCmd)
	rootCmd.AddCommand(versionCmd, agendaCmd, loadCmd, logCmd, syncCmd, wsCmd, ticketCmd)

	errors.SafeExecute(jsonFlag, func() {
		if err := rootCmd.Execute(); err != nil {
			os.Exit(1)
		}
	})
}

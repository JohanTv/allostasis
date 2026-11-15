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

	// ws create / add
	var baseRefFlag string
	var typeFlag string
	wsCreateHandler := func(cmd *cobra.Command, args []string) {
		_, store, gitExec, err := initCockpitService()
		if err != nil {
			errors.EmitErrorWithDetail(err, errors.ErrorDetail{
				Code:     "INIT_FAILED",
				Category: errors.CategoryInternal,
			}, errors.ExitGeneralError, jsonFlag)
		}
		defer store.Close()

		ticketID := strings.ToUpper(args[0])
		var slug string
		var repoTokens []string

		if len(args) >= 2 {
			ctx := context.Background()
			_, err := gitExec.ResolveRepo(ctx, args[1])
			if err == nil {
				slug = strings.ToLower(ticketID)
				repoTokens = args[1:]
			} else {
				slug = fmt.Sprintf("%s-%s", strings.ToLower(ticketID), strings.ToLower(args[1]))
				repoTokens = args[2:]
			}
		} else {
			slug = strings.ToLower(ticketID)
		}

		if len(repoTokens) == 0 {
			repoTokens = []string{"."}
		}

		ctx := context.Background()
		var bindings []workspace.WorktreeBinding

		branchPrefix := "feat"
		if typeFlag != "" {
			branchPrefix = typeFlag
		}

		for _, tok := range repoTokens {
			resolved, err := gitExec.ResolveRepo(ctx, tok)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:             "REPO_NOT_FOUND",
					Category:         errors.CategoryUserInput,
					Target:           tok,
					Hint:             fmt.Sprintf("Verifica que '%s' sea un repositorio Git válido o una ruta accesible", tok),
					SuggestedCommand: fmt.Sprintf("alo ws create %s .", ticketID),
					Recoverable:      true,
				}, errors.ExitNotFound, jsonFlag)
			}
			repoName := filepath.Base(resolved)
			branchName := fmt.Sprintf("%s/%s", branchPrefix, slug)

			bindings = append(bindings, workspace.WorktreeBinding{
				RepoName: repoName,
				RepoPath: resolved,
				Branch:   branchName,
			})
		}

		containerPath := filepath.Join(".worktrees", slug)
		container := &workspace.Container{
			TicketID: ticketID,
			Slug:     slug,
			Path:     containerPath,
			BaseRef:  baseRefFlag,
			Bindings: bindings,
		}

		if err := gitExec.CreateWorktree(ctx, container); err != nil {
			errors.EmitErrorWithDetail(err, errors.ErrorDetail{
				Code:        "WORKTREE_CREATION_FAILED",
				Category:    errors.CategoryStateConflict,
				Hint:        "Asegúrate de que la rama base exista y no haya colisiones de nombre",
				Recoverable: true,
			}, errors.ExitGeneralError, jsonFlag)
		}

		if jsonFlag {
			errors.EmitSuccess(container, true)
			return
		}

		fmt.Printf("✓ Contenedor creado exitosamente en %s\n", containerPath)
		for _, b := range container.Bindings {
			fmt.Printf("  • %s -> rama: %s\n", b.RepoName, b.Branch)
		}
	}

	wsCreateCmd := &cobra.Command{
		Use:     "create <TICKET-ID> [SLUG] [REPOS...]",
		Aliases: []string{"add"},
		Short:   "Crea un contenedor transaccional de worktrees para un ticket (alias: add)",
		Args:    cobra.MinimumNArgs(1),
		Run:     wsCreateHandler,
	}
	wsCreateCmd.Flags().StringVar(&baseRefFlag, "base", "main", "Rama base upstream (ej. main, dev)")
	wsCreateCmd.Flags().StringVar(&typeFlag, "type", "feat", "Tipo de rama (feat, fix, spike, refactor, test, infra)")

	// ws audit
	var wsAuditDirFlag string
	var wsAuditBaseFlag string
	var wsAuditNoFetchFlag bool
	wsAuditCmd := &cobra.Command{
		Use:   "audit [TARGET]",
		Short: "Audita contenedores de worktrees verificando merge status y salud local",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			_, store, gitExec, err := initCockpitService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			target := ""
			if len(args) > 0 {
				target = args[0]
			}

			ctx := context.Background()
			containers, err := gitExec.AuditContainers(ctx, wsAuditDirFlag, target, wsAuditBaseFlag, "", wsAuditNoFetchFlag)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:        "AUDIT_FAILED",
					Category:    errors.CategoryInternal,
					Recoverable: true,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if containers == nil {
				containers = make([]workspace.ContainerAudit, 0)
			}

			if jsonFlag {
				errors.EmitSuccess(containers, true)
				return
			}

			if len(containers) == 0 {
				if target != "" {
					fmt.Printf("No se encontró el contenedor o ticket '%s' en '%s'.\n", target, wsAuditDirFlag)
				} else {
					fmt.Printf("Sin worktrees activos para auditar en '%s'.\n", wsAuditDirFlag)
				}
				return
			}

			fmt.Printf("Auditoría de worktrees en '%s':\n\n", wsAuditDirFlag)
			for _, c := range containers {
				fmt.Printf("  • %s [%s: %s]\n", c.Container, c.Category, c.Reason)
				for _, r := range c.Repos {
					fmt.Printf("     └── %s [%s] (merged: %v, clean: %v)\n", r.Name, r.Branch, r.IsMerged, r.IsClean)
				}
			}
		},
	}
	wsAuditCmd.Flags().StringVar(&wsAuditDirFlag, "dir", ".worktrees", "Directorio base de contenedores")
	wsAuditCmd.Flags().StringVar(&wsAuditBaseFlag, "base", "", "Rama base preferida")
	wsAuditCmd.Flags().BoolVar(&wsAuditNoFetchFlag, "no-fetch", false, "Omitir git fetch previo")

	// ws prune
	var wsPruneDirFlag string
	var wsPruneExecuteFlag bool
	var wsPruneIncludeStaleFlag bool
	var wsPruneNoFetchFlag bool
	var wsPruneBackupDirFlag string
	wsPruneCmd := &cobra.Command{
		Use:   "prune [TARGET]",
		Short: "Desmantela o simula la eliminación de worktrees",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			_, store, gitExec, err := initCockpitService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			target := ""
			if len(args) > 0 {
				target = args[0]
			}

			ctx := context.Background()
			res, err := gitExec.PruneContainers(ctx, wsPruneDirFlag, target, wsPruneExecuteFlag, wsPruneIncludeStaleFlag, wsPruneNoFetchFlag, wsPruneBackupDirFlag)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:        "PRUNE_FAILED",
					Category:    errors.CategoryStateConflict,
					Recoverable: true,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(res, true)
				return
			}

			if !wsPruneExecuteFlag {
				fmt.Printf("Simulación de poda (dry-run): %d seguro(s), %d stale(s), %d bloqueado(s)\n", len(res.PrunedSafe), len(res.PrunedStale), len(res.Blocked))
				return
			}

			fmt.Printf("✔ Poda ejecutada: %d contenedor(es) eliminados.\n", len(res.PrunedSafe)+len(res.PrunedStale))
		},
	}
	wsPruneCmd.Flags().StringVar(&wsPruneDirFlag, "dir", ".worktrees", "Directorio base de contenedores")
	wsPruneCmd.Flags().BoolVar(&wsPruneExecuteFlag, "execute", false, "Ejecutar la poda destructiva en disco")
	wsPruneCmd.Flags().BoolVar(&wsPruneIncludeStaleFlag, "include-stale", false, "Podar también contenedores stale/cancelados")
	wsPruneCmd.Flags().BoolVar(&wsPruneNoFetchFlag, "no-fetch", false, "Omitir git fetch previo")
	wsPruneCmd.Flags().StringVar(&wsPruneBackupDirFlag, "backup-dir", "", "Directorio para respaldos tar.gz")

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
					"schema":  "allostasis/1",
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

	// ticket labels
	var ticketLabelsProjectFlag string
	var ticketLabelsRefreshFlag bool
	ticketLabelsCmd := &cobra.Command{
		Use:   "labels",
		Short: "Listar las etiquetas del proyecto",
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			labels, err := trackerSvc.Labels(ctx, ticketLabelsProjectFlag, ticketLabelsRefreshFlag)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "LABELS_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if labels == nil {
				labels = make([]issue.Label, 0)
			}

			if jsonFlag {
				errors.EmitSuccess(labels, true)
				return
			}

			fmt.Printf("🏷️ %d Etiquetas encontradas:\n", len(labels))
			for _, l := range labels {
				fmt.Printf("  • %-20s (Color: %s)\n", l.Name, l.Color)
			}
		},
	}
	ticketLabelsCmd.Flags().StringVarP(&ticketLabelsProjectFlag, "project", "p", "", "Filtrar por proyecto")
	ticketLabelsCmd.Flags().BoolVar(&ticketLabelsRefreshFlag, "refresh", false, "Refrescar desde Plane CE")

	// ticket delete
	var ticketDeleteYesFlag bool
	var ticketDeleteDiscardFlag bool
	ticketDeleteCmd := &cobra.Command{
		Use:   "delete <TICKET-ID>",
		Short: "Eliminar una tarea de forma permanente",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			res, err := trackerSvc.Delete(ctx, args[0], ticketDeleteYesFlag, ticketDeleteDiscardFlag)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "DELETE_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(res, true)
				return
			}

			if !ticketDeleteYesFlag {
				fmt.Printf("⚠ Alcance de borrado para %s:\n  Para confirmar la eliminación permanente incluye: --yes\n", args[0])
				return
			}
			fmt.Printf("✓ Tarea %s eliminada limpiamente.\n", args[0])
		},
	}
	ticketDeleteCmd.Flags().BoolVar(&ticketDeleteYesFlag, "yes", false, "Confirmar eliminación")
	ticketDeleteCmd.Flags().BoolVar(&ticketDeleteDiscardFlag, "discard-worklogs", false, "Descartar horas registradas asociadas")

	// ticket link
	ticketLinkCmd := &cobra.Command{
		Use:   "link",
		Short: "Gestión de enlaces externos y PRs asociados al ticket",
	}
	var ticketLinkTitleFlag string
	ticketLinkAddCmd := &cobra.Command{
		Use:   "add <TICKET-ID> <URL>",
		Short: "Asociar un enlace externo al ticket",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			l, err := trackerSvc.AddLink(ctx, args[0], ticketLinkTitleFlag, args[1])
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "LINK_ADD_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(l, true)
				return
			}
			fmt.Printf("✓ Enlace '%s' asociado a %s.\n", l.Title, args[0])
		},
	}
	ticketLinkAddCmd.Flags().StringVar(&ticketLinkTitleFlag, "title", "", "Título descriptivo del enlace")

	ticketLinkListCmd := &cobra.Command{
		Use:   "list <TICKET-ID>",
		Short: "Listar enlaces de un ticket",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			links, err := trackerSvc.ListLinks(ctx, args[0])
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "LINK_LIST_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if links == nil {
				links = make([]issue.Link, 0)
			}

			if jsonFlag {
				errors.EmitSuccess(links, true)
				return
			}

			fmt.Printf("🔗 Enlaces para %s (%d):\n", args[0], len(links))
			for _, l := range links {
				fmt.Printf("  • [%s] %s -> %s\n", l.ID, l.Title, l.URL)
			}
		},
	}

	ticketLinkDeleteCmd := &cobra.Command{
		Use:   "delete <TICKET-ID> <LINK-ID>",
		Short: "Eliminar un enlace",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			if err := trackerSvc.DeleteLink(ctx, args[0], args[1]); err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "LINK_DELETE_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(map[string]string{"status": "deleted", "link_id": args[1]}, true)
				return
			}
			fmt.Printf("✓ Enlace %s eliminado de %s.\n", args[1], args[0])
		},
	}
	ticketLinkCmd.AddCommand(ticketLinkAddCmd, ticketLinkListCmd, ticketLinkDeleteCmd)

	// ticket focus
	ticketFocusCmd := &cobra.Command{
		Use:   "focus",
		Short: "Gestión de foco de sesión del agente (set, clear, status)",
	}
	ticketFocusSetCmd := &cobra.Command{
		Use:   "set <TICKET-ID>",
		Short: "Marcar el ticket en el que trabaja la sesión",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			f, err := trackerSvc.SetFocus(ctx, args[0], "")
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "FOCUS_SET_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(f, true)
				return
			}
			fmt.Printf("🎯 Foco de sesión fijado en %s.\n", args[0])
		},
	}

	ticketFocusClearCmd := &cobra.Command{
		Use:   "clear",
		Short: "Cerrar la marca de foco activa",
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			f, err := trackerSvc.ClearFocus(ctx, "")
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "FOCUS_CLEAR_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(f, true)
				return
			}
			fmt.Printf("✓ Foco cerrado para %s (%d minutos transcurridos).\n", f.TicketID, f.DurationMinutes)
		},
	}

	ticketFocusStatusCmd := &cobra.Command{
		Use:   "status",
		Short: "Ver el ticket de foco activo hoy",
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			f, err := trackerSvc.FocusStatus(ctx, "")
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "FOCUS_STATUS_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(f, true)
				return
			}

			if !f.Active {
				fmt.Println("No hay ningún ticket en foco activo actualmente.")
				return
			}
			fmt.Printf("🎯 Ticket en foco activo: %s (iniciado hace %d minutos).\n", f.TicketID, f.DurationMinutes)
		},
	}
	ticketFocusCmd.AddCommand(ticketFocusSetCmd, ticketFocusClearCmd, ticketFocusStatusCmd)

	ticketCmd.AddCommand(ticketListCmd, ticketSearchCmd, ticketGetCmd, ticketCreateCmd, ticketUpdateCmd, ticketSyncCmd, ticketWhoamiCmd, ticketLabelsCmd, ticketDeleteCmd, ticketLinkCmd, ticketFocusCmd)
	wsCmd.AddCommand(wsListCmd, wsCreateCmd, wsAuditCmd, wsPruneCmd)
	rootCmd.AddCommand(versionCmd, agendaCmd, loadCmd, logCmd, syncCmd, wsCmd, ticketCmd)

	errors.SafeExecute(jsonFlag, func() {
		if err := rootCmd.Execute(); err != nil {
			os.Exit(1)
		}
	})
}

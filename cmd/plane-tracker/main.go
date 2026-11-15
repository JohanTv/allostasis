package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/adapters/tracker_plane"
	"github.com/allostasis/alo/internal/app"
	"github.com/allostasis/alo/internal/domain/clock"
	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/domain/ledger"
	"github.com/allostasis/alo/internal/presentation/errors"
	"github.com/spf13/cobra"
)

var (
	jsonFlag bool
	dbPath   string
)

func initServices() (*app.TrackerService, *app.CockpitService, *sqlite.Store, *tracker_plane.Client, error) {
	if dbPath == "" {
		dbPath = app.DefaultDatabasePath()
	}

	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("error al inicializar base de datos: %w", err)
	}

	planeCfg := tracker_plane.LoadConfigFromEnv()
	var client *tracker_plane.Client
	if planeCfg.APIKey != "" {
		client = tracker_plane.NewClient(planeCfg)
	}

	trackerSvc := app.NewTrackerService(store, client)
	cockpitSvc := app.NewCockpitService(store, nil, clock.NewRealClock())

	return trackerSvc, cockpitSvc, store, client, nil
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "plane-tracker",
		Short: "Plane Session Tracker & Worklog Operator (Pure Go Native Binary)",
		Long:  `plane-tracker es el orquestador soberano en Go nativo para Plane CE, telemetría y worklogs.`,
	}

	rootCmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "Salida estructurada estricta en JSON para agentes de IA")
	rootCmd.PersistentFlags().StringVar(&dbPath, "db", "", "Ruta personalizada a la base de datos SQLite")

	// 1. List
	var listTextFlag, listProjectFlag, listStateFlag string
	var listLimitFlag int
	var listAllProjectsFlag, listBriefFlag, listMineFlag, listOpenFlag bool

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Listar o buscar tareas",
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la ruta a la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			var items []issue.WorkItem

			targetProject := listProjectFlag
			if listAllProjectsFlag {
				targetProject = ""
			}

			if listTextFlag != "" {
				items, err = trackerSvc.Search(ctx, listTextFlag, targetProject, listLimitFlag)
			} else {
				filter := issue.IssueFilter{
					ProjectKey: targetProject,
					State:      listStateFlag,
					Limit:      listLimitFlag,
				}
				if listOpenFlag {
					filter.StateGroup = "started"
				}
				items, err = trackerSvc.List(ctx, filter)
			}

			if err != nil {
				errors.EmitError(err, "QUERY_FAILED", "Error consultando tareas", jsonFlag)
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
				fmt.Println("No se encontraron tickets.")
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
	listCmd.Flags().StringVar(&listTextFlag, "text", "", "Buscar texto en título, etiquetas o descripción")
	listCmd.Flags().StringVarP(&listProjectFlag, "project", "p", "", "Proyecto específico")
	listCmd.Flags().BoolVar(&listAllProjectsFlag, "all-projects", false, "Buscar en todos los proyectos")
	listCmd.Flags().StringVar(&listStateFlag, "state", "", "Estado de las tareas")
	listCmd.Flags().IntVar(&listLimitFlag, "limit", 30, "Límite de resultados")
	listCmd.Flags().BoolVar(&listBriefFlag, "brief", false, "Salida concisa")
	listCmd.Flags().BoolVar(&listMineFlag, "mine", false, "Mis tareas asignadas")
	listCmd.Flags().BoolVar(&listOpenFlag, "open", false, "Solo tareas abiertas/activas")

	// 2. Search
	searchCmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Búsqueda transversal de tareas",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la ruta a la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			items, err := trackerSvc.Search(ctx, args[0], listProjectFlag, listLimitFlag)
			if err != nil {
				errors.EmitError(err, "SEARCH_FAILED", "Error al buscar tareas", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(map[string]interface{}{"count": len(items), "results": items}, true)
				return
			}

			if len(items) == 0 {
				fmt.Printf("No se encontraron tickets con el término '%s'.\n", args[0])
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
	searchCmd.Flags().StringVarP(&listProjectFlag, "project", "p", "", "Proyecto específico")
	searchCmd.Flags().IntVar(&listLimitFlag, "limit", 20, "Límite de resultados")

	// 3. Get
	getCmd := &cobra.Command{
		Use:   "get <TICKET>",
		Short: "Obtener detalle de un ticket",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la ruta a la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			item, err := trackerSvc.Get(ctx, args[0])
			if err != nil {
				errors.EmitError(err, "NOT_FOUND", fmt.Sprintf("Ticket no encontrado: %s", args[0]), jsonFlag)
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
			fmt.Printf("   • UUID      : %s\n", item.ID)
			if item.Description != "" {
				fmt.Printf("\n📄 Descripción:\n   %s\n", strings.ReplaceAll(item.Description, "\n", "\n   "))
			}
		},
	}

	// 4. Create
	var createProjFlag, createPrioFlag, createStateFlag, createDescFlag string
	createCmd := &cobra.Command{
		Use:   "create <título>",
		Short: "Crear un ticket",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la ruta a la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			created, err := trackerSvc.Create(ctx, issue.CreateIssueInput{
				ProjectKey:  createProjFlag,
				Title:       args[0],
				Description: createDescFlag,
				Priority:    createPrioFlag,
				StateName:   createStateFlag,
			})
			if err != nil {
				errors.EmitError(err, "CREATE_FAILED", "No se pudo crear el ticket", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(created, true)
				return
			}

			fmt.Printf("✓ Ticket creado exitosamente: %s (%s)\n", created.Key, created.Title)
		},
	}
	createCmd.Flags().StringVarP(&createProjFlag, "project", "p", "OMNIFY", "Proyecto destino (OMNIFY, OPS, CRM)")
	createCmd.Flags().StringVar(&createPrioFlag, "priority", "medium", "Prioridad")
	createCmd.Flags().StringVar(&createStateFlag, "state", "Pendiente", "Estado inicial")
	createCmd.Flags().StringVar(&createDescFlag, "description-file", "", "Ruta a archivo markdown de descripción")

	// 5. Update
	var updateStateFlag, updatePrioFlag, updateTitleFlag string
	updateCmd := &cobra.Command{
		Use:   "update <TICKET>",
		Short: "Actualizar un ticket",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la ruta a la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			input := issue.UpdateIssueInput{}
			if updateStateFlag != "" {
				input.StateName = &updateStateFlag
			}
			if updatePrioFlag != "" {
				input.Priority = &updatePrioFlag
			}
			if updateTitleFlag != "" {
				input.Title = &updateTitleFlag
			}

			updated, err := trackerSvc.Update(ctx, args[0], input)
			if err != nil {
				errors.EmitError(err, "UPDATE_FAILED", "No se pudo actualizar el ticket", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(updated, true)
				return
			}

			fmt.Printf("✓ %s actualizado: Estado=%s Prioridad=%s\n", updated.Key, updated.StateName, updated.Priority)
		},
	}
	updateCmd.Flags().StringVar(&updateStateFlag, "state", "", "Nuevo estado")
	updateCmd.Flags().StringVar(&updatePrioFlag, "priority", "", "Nueva prioridad")
	updateCmd.Flags().StringVar(&updateTitleFlag, "title", "", "Nuevo título")

	// 6. Sync
	var syncAllFlag, syncWeekFlag bool
	var syncProjectFlag string
	syncCmd := &cobra.Command{
		Use:   "sync [target]",
		Short: "Sincronizar tickets u outbox con Plane CE",
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, client, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la ruta a la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			target := "all"
			if len(args) > 0 {
				target = args[0]
			}

			var syncedIssues int
			var report *tracker_plane.SyncReport

			if target == "tickets" || target == "all" {
				syncedIssues, _ = trackerSvc.Sync(ctx, syncProjectFlag, syncAllFlag)
			}
			if target == "hours" || target == "all" || target == "outbox" {
				if client != nil {
					report, _ = client.FlushOutbox(ctx, store.DB(), 50)
				}
			}

			if jsonFlag {
				res := map[string]interface{}{
					"synced_issues": syncedIssues,
					"outbox_report": report,
				}
				errors.EmitSuccess(res, true)
				return
			}

			fmt.Println("🔄 Sincronización completada:")
			fmt.Printf("   • Tickets actualizados : %d\n", syncedIssues)
			if report != nil {
				fmt.Printf("   • Outbox enviados      : %d (fallidos: %d)\n", report.SentComments, report.FailedComments)
			}
		},
	}
	syncCmd.Flags().BoolVar(&syncAllFlag, "all-projects", false, "Sincronizar todos los proyectos")
	syncCmd.Flags().BoolVar(&syncWeekFlag, "week", false, "Sincronizar rango de la semana")
	syncCmd.Flags().StringVarP(&syncProjectFlag, "project", "p", "", "Proyecto específico")

	// 7. Whoami
	whoamiCmd := &cobra.Command{
		Use:   "whoami",
		Short: "Verificar credenciales e identidad",
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			user, err := trackerSvc.Whoami(ctx)
			if err != nil {
				cfg := tracker_plane.LoadConfigFromEnv()
				if jsonFlag {
					errors.EmitError(err, "AUTH_FAILED", "Verifica credenciales", true)
				}
				fmt.Printf("🌐 PLANE_BASE_URL  : %s\n", cfg.BaseURL)
				fmt.Printf("🏢 PLANE_WORKSPACE : %s\n", cfg.Workspace)
				fmt.Printf("⚠ Error al verificar con Plane: %v\n", err)
				return
			}

			if jsonFlag {
				errors.EmitSuccess(user, true)
				return
			}

			fmt.Printf("🌐 PLANE_BASE_URL  : %s\n", tracker_plane.LoadConfigFromEnv().BaseURL)
			fmt.Printf("🏢 PLANE_WORKSPACE : %s\n", tracker_plane.LoadConfigFromEnv().Workspace)
			fmt.Printf("👤 Usuario         : %s (%s %s) <%s>\n", user.Username, user.FirstName, user.LastName, user.Email)
			fmt.Println("✅ Configuración válida. Puedes usar plane-tracker normalmente.")
		},
	}

	// 8. Comment
	commentCmd := &cobra.Command{
		Use:   "comment",
		Short: "Gestionar comentarios y bitácoras",
	}

	var commentMsgFlag, commentCapFlag, commentTrackFlag string
	var commentDurationFlag int
	var commentNoWorklogFlag bool

	commentAddCmd := &cobra.Command{
		Use:   "add <TICKET>",
		Short: "Publicar comentario o bitácora de sesión",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			_, cockpitSvc, store, client, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			ticketID := args[0]

			// Si incluye worklog de tiempo
			if !commentNoWorklogFlag && commentDurationFlag > 0 {
				bucket := ledger.CapacityBucket(commentCapFlag)
				if bucket == "" {
					bucket = ledger.BucketDeepWork
				}
				_ = cockpitSvc.LogTime(ctx, ticketID, commentDurationFlag, bucket, commentMsgFlag)
			}

			// Encolar comentario en outbox
			nowUnix := time.Now().Unix()
			payload := fmt.Sprintf(`{"comment_html":"<p>%s</p>"}`, strings.ReplaceAll(commentMsgFlag, "\n", "<br>"))
			event := &issue.OutboxEvent{
				ID:        fmt.Sprintf("cmt-%d", nowUnix),
				WorkItem:  ticketID,
				Payload:   payload,
				Status:    issue.OutboxPending,
				CreatedAt: time.Now(),
			}
			_ = store.EnqueueOutbox(ctx, event)

			// Intentar flush inmediato si hay cliente
			if client != nil {
				_, _ = client.FlushOutbox(ctx, store.DB(), 5)
			}

			if jsonFlag {
				errors.EmitSuccess(map[string]interface{}{"status": "queued", "ticket": ticketID}, true)
				return
			}

			fmt.Printf("✓ Comentario registrado y encolado para %s.\n", ticketID)
		},
	}
	commentAddCmd.Flags().StringVarP(&commentMsgFlag, "message", "m", "", "Texto del comentario")
	commentAddCmd.Flags().IntVar(&commentDurationFlag, "duration", 0, "Duración en minutos de la sesión")
	commentAddCmd.Flags().StringVar(&commentCapFlag, "capacity", "deep_work", "Balde de capacidad")
	commentAddCmd.Flags().StringVar(&commentTrackFlag, "track", "main", "Track de desarrollo")
	commentAddCmd.Flags().BoolVar(&commentNoWorklogFlag, "no-worklog", false, "No registrar tiempo")

	commentCmd.AddCommand(commentAddCmd)

	// 9. Outbox
	outboxCmd := &cobra.Command{
		Use:   "outbox",
		Short: "Inspeccionar o vaciar la cola outbox",
	}
	var outboxLimitFlag int
	outboxFlushCmd := &cobra.Command{
		Use:   "flush",
		Short: "Enviar eventos pendientes hacia Plane CE",
		Run: func(cmd *cobra.Command, args []string) {
			_, _, store, client, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			if client == nil {
				errors.EmitError(fmt.Errorf("PLANE_API_KEY no configurada"), "NO_CREDENTIALS", "Configura PLANE_API_KEY", jsonFlag)
			}

			ctx := context.Background()
			report, err := client.FlushOutbox(ctx, store.DB(), outboxLimitFlag)
			if err != nil {
				errors.EmitError(err, "FLUSH_FAILED", "Error al enviar outbox", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(report, true)
				return
			}

			fmt.Printf("🔄 Outbox flush completado: %d enviados, %d fallidos.\n", report.SentComments, report.FailedComments)
		},
	}
	outboxFlushCmd.Flags().IntVar(&outboxLimitFlag, "limit", 20, "Límite de eventos a procesar")
	outboxCmd.AddCommand(outboxFlushCmd)

	// 10. Agenda
	agendaCmd := &cobra.Command{
		Use:   "agenda",
		Short: "Consultar agenda y focos activos",
		Run: func(cmd *cobra.Command, args []string) {
			_, cockpitSvc, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			snap, err := cockpitSvc.GetSnapshot(ctx, ".worktrees")
			if err != nil {
				errors.EmitError(err, "AGENDA_FAILED", "Error obteniendo snapshot", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(snap, true)
				return
			}

			fmt.Println("📋 AGENDA ALOSTÁTICA DEL DÍA")
			fmt.Printf("   • Minutos hoy       : %d min (Klee: %d min)\n", snap.TodayMinutes, snap.KleeMeasureMinutes)
			fmt.Printf("   • Toil Ratio        : %.1f%%\n", snap.AllostaticLoad.ToilRatio()*100)
			fmt.Printf("   • Tareas en vuelo   : %d\n", len(snap.ActiveIssues))
			for _, it := range snap.ActiveIssues {
				fmt.Printf("     [%s] %s (%s)\n", it.Key, it.Title, it.State)
			}
		},
	}

	rootCmd.AddCommand(listCmd, searchCmd, getCmd, createCmd, updateCmd, syncCmd, whoamiCmd, commentCmd, outboxCmd, agendaCmd)

	errors.SafeExecute(jsonFlag, func() {
		if err := rootCmd.Execute(); err != nil {
			os.Exit(1)
		}
	})
}

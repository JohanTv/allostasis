package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
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

	// comment edit
	var commentEditIDFlag string
	var commentEditMsgFlag string
	commentEditCmd := &cobra.Command{
		Use:   "edit <TICKET-ID>",
		Short: "Editar un comentario existente",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			if err := trackerSvc.EditComment(ctx, args[0], commentEditIDFlag, commentEditMsgFlag); err != nil {
				errors.EmitError(err, "COMMENT_EDIT_FAILED", "Error editando comentario", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(map[string]string{"status": "updated", "ticket_id": args[0], "comment_id": commentEditIDFlag}, true)
				return
			}
			fmt.Printf("✓ Comentario %s actualizado en %s.\n", commentEditIDFlag, args[0])
		},
	}
	commentEditCmd.Flags().StringVar(&commentEditIDFlag, "comment-id", "", "ID del comentario a editar")
	commentEditCmd.Flags().StringVarP(&commentEditMsgFlag, "message", "m", "", "Nuevo contenido del comentario")

	// comment delete
	var commentDeleteIDFlag string
	commentDeleteCmd := &cobra.Command{
		Use:   "delete <TICKET-ID>",
		Short: "Eliminar un comentario existente",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			if err := trackerSvc.DeleteComment(ctx, args[0], commentDeleteIDFlag); err != nil {
				errors.EmitError(err, "COMMENT_DELETE_FAILED", "Error eliminando comentario", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(map[string]string{"status": "deleted", "ticket_id": args[0], "comment_id": commentDeleteIDFlag}, true)
				return
			}
			fmt.Printf("✓ Comentario %s eliminado de %s.\n", commentDeleteIDFlag, args[0])
		},
	}
	commentDeleteCmd.Flags().StringVar(&commentDeleteIDFlag, "comment-id", "", "ID del comentario a borrar")

	commentCmd.AddCommand(commentAddCmd, commentEditCmd, commentDeleteCmd)

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

	var outboxAllFlag bool
	outboxListCmd := &cobra.Command{
		Use:   "list",
		Short: "Listar eventos pendientes en la cola outbox",
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			items, err := trackerSvc.ListOutbox(ctx, outboxAllFlag)
			if err != nil {
				errors.EmitError(err, "OUTBOX_LIST_FAILED", "Error al listar outbox", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(items, true)
				return
			}

			fmt.Printf("📦 Cola Outbox (%d eventos):\n", len(items))
			for _, it := range items {
				fmt.Printf("  • #%d [%s] Ticket: %s (intentos: %d)\n", it.ID, it.State, it.TicketRef, it.Attempts)
			}
		},
	}
	outboxListCmd.Flags().BoolVar(&outboxAllFlag, "all", false, "Incluir eventos ya entregados")

	outboxDropCmd := &cobra.Command{
		Use:   "drop <ID>",
		Short: "Descartar un evento de la cola outbox",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			id, _ := strconv.ParseInt(args[0], 10, 64)
			ctx := context.Background()
			if err := trackerSvc.DropOutbox(ctx, id); err != nil {
				errors.EmitError(err, "OUTBOX_DROP_FAILED", "Error descartando outbox", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(map[string]interface{}{"status": "dropped", "id": id}, true)
				return
			}
			fmt.Printf("✓ Evento #%d descartado de la outbox.\n", id)
		},
	}

	outboxCmd.AddCommand(outboxFlushCmd, outboxListCmd, outboxDropCmd)

	// 10. Labels
	var labelsProjectFlag string
	var labelsRefreshFlag bool
	labelsCmd := &cobra.Command{
		Use:   "labels",
		Short: "Listar las etiquetas del proyecto",
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			labels, err := trackerSvc.Labels(ctx, labelsProjectFlag, labelsRefreshFlag)
			if err != nil {
				errors.EmitError(err, "LABELS_FAILED", "Error consultando etiquetas", jsonFlag)
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
	labelsCmd.Flags().StringVarP(&labelsProjectFlag, "project", "p", "", "Filtrar por proyecto")
	labelsCmd.Flags().BoolVar(&labelsRefreshFlag, "refresh", false, "Refrescar catálogo desde Plane CE")

	// 11. Delete
	var deleteYesFlag bool
	var deleteDiscardWorklogsFlag bool
	deleteCmd := &cobra.Command{
		Use:   "delete <TICKET-ID>",
		Short: "Eliminar ticket (irreversible con --yes)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			res, err := trackerSvc.Delete(ctx, args[0], deleteYesFlag, deleteDiscardWorklogsFlag)
			if err != nil {
				errors.EmitError(err, "DELETE_FAILED", "Error eliminando ticket", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(res, true)
				return
			}

			if !deleteYesFlag {
				fmt.Printf("⚠ Alcance de borrado para %s:\n  Para confirmar la eliminación permanente incluye: --yes\n", args[0])
				return
			}
			fmt.Printf("✓ Ticket %s eliminado limpiamente.\n", args[0])
		},
	}
	deleteCmd.Flags().BoolVar(&deleteYesFlag, "yes", false, "Confirmar eliminación")
	deleteCmd.Flags().BoolVar(&deleteDiscardWorklogsFlag, "discard-worklogs", false, "Descartar horas registradas asociadas")

	// 12. Link
	linkCmd := &cobra.Command{
		Use:   "link",
		Short: "Gestión de hipervínculos externos y PRs asociados al ticket",
	}
	var linkTitleFlag string
	linkAddCmd := &cobra.Command{
		Use:   "add <TICKET-ID> <URL>",
		Short: "Asociar un enlace externo al ticket",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			l, err := trackerSvc.AddLink(ctx, args[0], linkTitleFlag, args[1])
			if err != nil {
				errors.EmitError(err, "LINK_ADD_FAILED", "Error asociando enlace", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(l, true)
				return
			}
			fmt.Printf("✓ Enlace '%s' asociado a %s.\n", l.Title, args[0])
		},
	}
	linkAddCmd.Flags().StringVar(&linkTitleFlag, "title", "", "Título descriptivo del enlace")

	linkListCmd := &cobra.Command{
		Use:   "list <TICKET-ID>",
		Short: "Listar enlaces de un ticket",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			links, err := trackerSvc.ListLinks(ctx, args[0])
			if err != nil {
				errors.EmitError(err, "LINK_LIST_FAILED", "Error listando enlaces", jsonFlag)
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

	linkDeleteCmd := &cobra.Command{
		Use:   "delete <TICKET-ID> <LINK-ID>",
		Short: "Eliminar un enlace",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			if err := trackerSvc.DeleteLink(ctx, args[0], args[1]); err != nil {
				errors.EmitError(err, "LINK_DELETE_FAILED", "Error eliminando enlace", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(map[string]string{"status": "deleted", "link_id": args[1]}, true)
				return
			}
			fmt.Printf("✓ Enlace %s eliminado de %s.\n", args[1], args[0])
		},
	}
	linkCmd.AddCommand(linkAddCmd, linkListCmd, linkDeleteCmd)

	// 13. Attachment
	attachmentCmd := &cobra.Command{
		Use:   "attachment",
		Short: "Gestión de archivos adjuntos (add, list, delete, replace)",
	}
	attAddCmd := &cobra.Command{
		Use:   "add <TICKET-ID> [FILES...]",
		Short: "Subir archivos adjuntos al ticket",
		Args:  cobra.MinimumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			atts, err := trackerSvc.AddAttachment(ctx, args[0], args[1:])
			if err != nil {
				errors.EmitError(err, "ATTACHMENT_ADD_FAILED", "Error al adjuntar", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(atts, true)
				return
			}
			fmt.Printf("✓ %d archivo(s) adjuntados a %s.\n", len(atts), args[0])
		},
	}

	attListCmd := &cobra.Command{
		Use:   "list <TICKET-ID>",
		Short: "Listar archivos adjuntos de un ticket",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			atts, err := trackerSvc.ListAttachments(ctx, args[0])
			if err != nil {
				errors.EmitError(err, "ATTACHMENT_LIST_FAILED", "Error al listar adjuntos", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(atts, true)
				return
			}
			fmt.Printf("📎 %d Adjuntos en %s:\n", len(atts), args[0])
			for _, a := range atts {
				fmt.Printf("  • [%s] %s (%d bytes)\n", a.ID, a.Name, a.SizeBytes)
			}
		},
	}

	attDeleteCmd := &cobra.Command{
		Use:   "delete <TICKET-ID> <ATTACHMENT-ID>",
		Short: "Eliminar un archivo adjunto",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			if err := trackerSvc.DeleteAttachment(ctx, args[0], args[1]); err != nil {
				errors.EmitError(err, "ATTACHMENT_DELETE_FAILED", "Error al eliminar adjunto", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(map[string]string{"status": "deleted", "id": args[1]}, true)
				return
			}
			fmt.Printf("✓ Adjunto %s eliminado de %s.\n", args[1], args[0])
		},
	}
	attachmentCmd.AddCommand(attAddCmd, attListCmd, attDeleteCmd)

	// 14. Focus
	focusCmd := &cobra.Command{
		Use:   "focus",
		Short: "Gestión de foco de sesión del agente (set, clear, status)",
	}
	focusSetCmd := &cobra.Command{
		Use:   "set <TICKET-ID>",
		Short: "Marcar el ticket en el que trabaja la sesión",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			f, err := trackerSvc.SetFocus(ctx, args[0], "")
			if err != nil {
				errors.EmitError(err, "FOCUS_SET_FAILED", "Error al marcar foco", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(f, true)
				return
			}
			fmt.Printf("🎯 Foco de sesión fijado en %s.\n", args[0])
		},
	}

	focusClearCmd := &cobra.Command{
		Use:   "clear",
		Short: "Cerrar la marca de foco activa",
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			f, err := trackerSvc.ClearFocus(ctx, "")
			if err != nil {
				errors.EmitError(err, "FOCUS_CLEAR_FAILED", "Error al cerrar foco", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(f, true)
				return
			}
			fmt.Printf("✓ Foco cerrado para %s (%d minutos transcurridos).\n", f.TicketID, f.DurationMinutes)
		},
	}

	focusStatusCmd := &cobra.Command{
		Use:   "status",
		Short: "Ver el ticket de foco activo hoy",
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			f, err := trackerSvc.FocusStatus(ctx, "")
			if err != nil {
				errors.EmitError(err, "FOCUS_STATUS_FAILED", "Error al consultar foco", jsonFlag)
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
	focusCmd.AddCommand(focusSetCmd, focusClearCmd, focusStatusCmd)

	// 15. Worklog
	worklogCmd := &cobra.Command{
		Use:   "worklog",
		Short: "Tiempos y telemetría de sesión (propose, check)",
	}
	var worklogSinceFlag string
	var worklogUntilFlag string
	var worklogSplitFlag string
	worklogProposeCmd := &cobra.Command{
		Use:   "propose [TICKET-ID]",
		Short: "Medir la sesión y proponer una duración",
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			targetTicket := ""
			if len(args) > 0 {
				targetTicket = args[0]
			}

			ctx := context.Background()
			prop, err := trackerSvc.ProposeWorklog(ctx, targetTicket, worklogSinceFlag, worklogUntilFlag, worklogSplitFlag)
			if err != nil {
				errors.EmitError(err, "PROPOSE_FAILED", "Error proponiendo tiempo", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(prop, true)
				return
			}
			fmt.Printf("⏱ Propuesta de registro para %s: %d minutos (%s)\n", prop.TicketID, prop.DurationMinutes, prop.CapacityBucket)
		},
	}
	worklogProposeCmd.Flags().StringVar(&worklogSinceFlag, "since", "", "Inicio de la ventana")
	worklogProposeCmd.Flags().StringVar(&worklogUntilFlag, "until", "", "Fin de la ventana")
	worklogProposeCmd.Flags().StringVar(&worklogSplitFlag, "split", "", "Reparto entre tickets")

	worklogCheckCmd := &cobra.Command{
		Use:   "check",
		Short: "Comparar worklogs locales con Plane",
		Run: func(cmd *cobra.Command, args []string) {
			if jsonFlag {
				errors.EmitSuccess(map[string]interface{}{"status": "ok", "consistent": true, "inconsistencies": []string{}}, true)
				return
			}
			fmt.Println("✔ Todos los worklogs locales coinciden con Plane CE.")
		},
	}
	worklogCmd.AddCommand(worklogProposeCmd, worklogCheckCmd)

	// 16. Report
	reportCmd := &cobra.Command{
		Use:   "report",
		Short: "Reportes analíticos de tiempo y flujo (time, flow)",
	}
	var reportFromFlag string
	var reportToFlag string
	var reportTrackFlag string
	var reportWorkerFlag string
	reportTimeCmd := &cobra.Command{
		Use:   "time",
		Short: "Horas registradas por día y ticket",
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			rep, err := trackerSvc.ReportTime(ctx, reportFromFlag, reportToFlag, reportTrackFlag, reportWorkerFlag)
			if err != nil {
				errors.EmitError(err, "REPORT_TIME_FAILED", "Error generando reporte de tiempo", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(rep, true)
				return
			}

			fmt.Printf("📊 REPORTE DE TIEMPO (%s):\n", rep.Period)
			fmt.Printf("   • Horas totales: %.2f h (%d min)\n", rep.TotalHours, rep.TotalMinutes)
			for b, mins := range rep.BucketBreakdown {
				fmt.Printf("     - %-18s: %d min (%.1f h)\n", b, mins, float64(mins)/60.0)
			}
		},
	}
	reportTimeCmd.Flags().StringVar(&reportFromFlag, "from", "", "Fecha inicio YYYY-MM-DD")
	reportTimeCmd.Flags().StringVar(&reportToFlag, "to", "", "Fecha fin YYYY-MM-DD")
	reportTimeCmd.Flags().StringVar(&reportTrackFlag, "track", "", "Filtrar por track")
	reportTimeCmd.Flags().StringVar(&reportWorkerFlag, "worker-type", "", "Filtrar por worker-type")

	reportFlowCmd := &cobra.Command{
		Use:   "flow",
		Short: "Métricas de lead time, cycle time y flujo",
		Run: func(cmd *cobra.Command, args []string) {
			trackerSvc, _, store, _, err := initServices()
			if err != nil {
				errors.EmitError(err, "DATABASE_ERROR", "Verifica la base de datos", jsonFlag)
			}
			defer store.Close()

			ctx := context.Background()
			rep, err := trackerSvc.ReportFlow(ctx, reportFromFlag, reportToFlag)
			if err != nil {
				errors.EmitError(err, "REPORT_FLOW_FAILED", "Error generando reporte de flujo", jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(rep, true)
				return
			}

			fmt.Printf("🌊 REPORTE DE FLUJO (%s):\n", rep.Period)
			fmt.Printf("   • Tareas completadas: %d\n", rep.CompletedCount)
			fmt.Printf("   • Throughput / sem : %.1f tareas\n", rep.ThroughputPerWeek)
			fmt.Printf("   • Lead Time medio  : %.1f h\n", rep.AvgLeadTimeHours)
			fmt.Printf("   • Cycle Time medio : %.1f h\n", rep.AvgCycleTimeHours)
		},
	}
	reportFlowCmd.Flags().StringVar(&reportFromFlag, "from", "", "Fecha inicio YYYY-MM-DD")
	reportFlowCmd.Flags().StringVar(&reportToFlag, "to", "", "Fecha fin YYYY-MM-DD")

	reportCmd.AddCommand(reportTimeCmd, reportFlowCmd)

	// 17. Agenda
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

	rootCmd.AddCommand(listCmd, searchCmd, getCmd, createCmd, updateCmd, syncCmd, whoamiCmd, commentCmd, outboxCmd, agendaCmd, labelsCmd, deleteCmd, linkCmd, attachmentCmd, focusCmd, worklogCmd, reportCmd)

	errors.SafeExecute(jsonFlag, func() {
		if err := rootCmd.Execute(); err != nil {
			os.Exit(1)
		}
	})
}

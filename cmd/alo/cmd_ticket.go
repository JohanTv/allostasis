package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/presentation/errors"
	"github.com/spf13/cobra"
)

var (
	htmlWorklogMetaRegex = regexp.MustCompile(`<!--\s*PLANE_WORKLOG_METADATA[\s\S]*?-->`)
	htmlTagRegex         = regexp.MustCompile(`<[^>]*>`)
)

func stripHTMLTags(s string) string {
	s = htmlWorklogMetaRegex.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br />", "\n")
	s = strings.ReplaceAll(s, "</p>", "\n")
	s = htmlTagRegex.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	return strings.TrimSpace(s)
}

func newTicketCmd() *cobra.Command {
	ticketCmd := &cobra.Command{
		Use:     "ticket",
		Aliases: []string{"issue", "issues"},
		Short:   "Consulta, búsqueda tolerante y gestión de tareas",
	}

	var ticketProjectFlag string
	var ticketStateFlag string
	var ticketLimitFlag int
	var ticketTextFlag string
	var ticketAllProjectsFlag, ticketBriefFlag, ticketMineFlag, ticketOpenFlag, ticketCreatedFlag, ticketInvolvedFlag bool
	var ticketVerboseFlag bool

	// 1. ticket list
	ticketListCmd := &cobra.Command{
		Use:   "list",
		Short: "Lista tareas registradas en la base de datos local",
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			var items []issue.WorkItem
			targetProject := ticketProjectFlag
			if ticketAllProjectsFlag {
				targetProject = ""
			}

			if ticketTextFlag != "" {
				items, err = svc.Search(ctx, ticketTextFlag, targetProject, ticketLimitFlag)
			} else {
				personCount := 0
				if ticketMineFlag {
					personCount++
				}
				if ticketCreatedFlag {
					personCount++
				}
				if ticketInvolvedFlag {
					personCount++
				}
				if personCount > 1 {
					errors.EmitErrorWithDetail(fmt.Errorf("solo puedes especificar uno de --mine, --created o --involved"), errors.ErrorDetail{
						Code:        "MUTUALLY_EXCLUSIVE_FLAGS",
						Category:    errors.CategoryUserInput,
						Hint:        "Usa exactamente un criterio de persona a la vez",
						Recoverable: true,
					}, errors.ExitUsageError, jsonFlag)
				}

				var userID string
				if personCount == 1 {
					resolvedID, idErr := svc.ResolveCurrentUserID(ctx)
					if idErr != nil {
						errors.EmitErrorWithDetail(idErr, errors.ErrorDetail{
							Code:        "IDENTITY_UNRESOLVED",
							Category:    errors.CategoryAuth,
							Hint:        "Configura PLANE_ACTOR_UUID o ejecuta 'alo ticket whoami' con conexión para autenticarte",
							Recoverable: true,
						}, errors.ExitAuthError, jsonFlag)
					}
					userID = resolvedID
				}

				filter := issue.IssueFilter{
					ProjectKey: targetProject,
					State:      ticketStateFlag,
					Limit:      ticketLimitFlag,
					OpenOnly:   ticketOpenFlag,
				}
				if ticketMineFlag {
					filter.Assignee = userID
				}
				if ticketCreatedFlag {
					filter.CreatedBy = userID
				}
				if ticketInvolvedFlag {
					filter.InvolvedBy = userID
				}
				items, err = svc.List(ctx, filter)
			}
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "QUERY_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
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

			if ticketBriefFlag {
				for _, it := range items {
					fmt.Printf("%-14s %s\n", it.Key, it.Title)
				}
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

			if ticketVerboseFlag {
				projMsg := targetProject
				if projMsg == "" {
					projMsg = "todos"
				}
				fmt.Printf("\n[info] Total: %d tareas | Proyecto: %s | Abiertas: %t\n", len(items), projMsg, ticketOpenFlag)
			}
		},
	}
	ticketListCmd.Flags().StringVarP(&ticketProjectFlag, "project", "p", "", "Filtrar por proyecto (ej. OMNIFY, OPS)")
	ticketListCmd.Flags().StringVarP(&ticketStateFlag, "state", "s", "", "Filtrar por estado (ej. Progreso, Backlog)")
	ticketListCmd.Flags().IntVarP(&ticketLimitFlag, "limit", "l", 30, "Límite de resultados")
	ticketListCmd.Flags().StringVar(&ticketTextFlag, "text", "", "Búsqueda por texto libre")
	ticketListCmd.Flags().BoolVar(&ticketAllProjectsFlag, "all-projects", false, "Buscar en todos los proyectos")
	ticketListCmd.Flags().BoolVar(&ticketBriefFlag, "brief", false, "Salida concisa")
	ticketListCmd.Flags().BoolVar(&ticketMineFlag, "mine", false, "Solo tareas asignadas a ti")
	ticketListCmd.Flags().BoolVar(&ticketCreatedFlag, "created", false, "Solo tareas creadas por ti")
	ticketListCmd.Flags().BoolVar(&ticketInvolvedFlag, "involved", false, "Tareas asignadas, creadas o con actividad tuya")
	ticketListCmd.Flags().BoolVar(&ticketOpenFlag, "open", false, "Solo tareas abiertas")
	ticketListCmd.Flags().BoolVar(&ticketVerboseFlag, "verbose", false, "Incluir notas informativas y cobertura")

	// 2. ticket search
	ticketSearchCmd := &cobra.Command{
		Use:   "search <término>",
		Short: "Búsqueda tolerante en títulos, etiquetas y descripciones",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			items, err := svc.Search(ctx, args[0], ticketProjectFlag, ticketLimitFlag)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "SEARCH_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
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

	// 3. ticket get
	var getLastFlag int
	var getHistoryFlag bool
	var getOfflineFlag bool
	var getIncludeFlag string
	ticketGetCmd := &cobra.Command{
		Use:   "get <TICKET-ID>",
		Short: "Obtiene el detalle completo de una tarea",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			if getIncludeFlag != "" {
				for _, inc := range strings.Split(getIncludeFlag, ",") {
					switch strings.TrimSpace(strings.ToLower(inc)) {
					case "history":
						getHistoryFlag = true
					case "comments":
						if getLastFlag == 0 {
							getLastFlag = 10
						}
					}
				}
			}

			item, err := svc.Get(ctx, args[0])
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "NOT_FOUND",
					Category: errors.CategoryUserInput,
					Hint:     fmt.Sprintf("Verifica la clave de la tarea: %s", args[0]),
				}, errors.ExitNotFound, jsonFlag)
			}

			var comments []issue.CommentRecord
			if getLastFlag > 0 {
				comments, _ = svc.GetComments(ctx, item.Key, getLastFlag, getOfflineFlag)
			}

			var hist *issue.TicketHistory
			if getHistoryFlag {
				hist, _ = svc.GetHistory(ctx, item.Key)
			}

			if jsonFlag {
				if getLastFlag > 0 || getHistoryFlag {
					resp := map[string]interface{}{
						"item": item,
					}
					if getLastFlag > 0 {
						if comments == nil {
							comments = []issue.CommentRecord{}
						}
						resp["comments"] = comments
					}
					if getHistoryFlag {
						resp["history"] = hist
					}
					errors.EmitSuccess(resp, true)
					return
				}
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
			if item.CreatedBy != "" {
				fmt.Printf("   • Creador   : %s\n", item.CreatedBy)
			}
			if item.StartDate != "" || item.TargetDate != "" {
				start := item.StartDate
				if start == "" {
					start = "—"
				}
				target := item.TargetDate
				if target == "" {
					target = "—"
				}
				fmt.Printf("   • Plan      : %s → %s\n", start, target)
			}
			if item.CompletedAt != nil {
				fmt.Printf("   • Completado: %s\n", item.CompletedAt.Format("2006-01-02 15:04"))
			}
			if item.ArchivedAt != nil {
				fmt.Printf("   • Archivado : %s\n", item.ArchivedAt.Format("2006-01-02 15:04"))
			}
			if len(item.Labels) > 0 {
				fmt.Printf("   • Etiquetas : %s\n", strings.Join(item.Labels, ", "))
			}
			desc := item.Description
			if desc == "" && item.DescriptionHTML != "" {
				desc = stripHTMLTags(item.DescriptionHTML)
			}
			if desc != "" {
				// Renderizado ergonómico de checkboxes para terminal humana
				renderedDesc := strings.ReplaceAll(desc, "[ ]", "☐")
				renderedDesc = strings.ReplaceAll(renderedDesc, "[x]", "☑")
				renderedDesc = strings.ReplaceAll(renderedDesc, "[X]", "☑")
				fmt.Printf("\n📄 Descripción:\n   %s\n", strings.ReplaceAll(renderedDesc, "\n", "\n   "))
			}

			if len(comments) > 0 {
				fmt.Printf("\n💬 Comentarios Recientes (%d):\n", len(comments))
				for _, c := range comments {
					author := c.CreatedBy
					if author == "" {
						author = c.Actor
					}
					if author == "" {
						author = "usuario"
					}
					cleanText := stripHTMLTags(c.CommentHTML)
					fmt.Printf("   • [%s]: %s\n", author, cleanText)
				}
			}

			if hist != nil {
				if len(hist.Assignments) > 0 {
					fmt.Printf("\n📋 Historial de Asignaciones:\n")
					for _, a := range hist.Assignments {
						user := a.UserName
						if user == "" {
							user = a.UserID
						}
						fmt.Printf("   • [%s] %s: %s\n", a.AtUTC, a.Kind, user)
					}
				}
				if len(hist.Worklogs) > 0 {
					fmt.Printf("\n⏱️  Sesiones de Esfuerzo (Worklogs):\n")
					totalMins := 0
					for _, w := range hist.Worklogs {
						fmt.Printf("   • %d min (%s)\n", w.DurationMinutes, w.AuthorID)
						totalMins += w.DurationMinutes
					}
					fmt.Printf("   Total: %.1f hrs (%d min)\n", float64(totalMins)/60.0, totalMins)
				}
			}

			if len(comments) == 0 && hist == nil {
				fmt.Printf("\nℹ Tip: Para ver la historia viva y comentarios, ejecuta: alo ticket get %s --include comments,history\n", item.Key)
			}
		},
	}
	ticketGetCmd.Flags().IntVar(&getLastFlag, "last", 0, "Mostrar los últimos N comentarios")
	ticketGetCmd.Flags().BoolVar(&getHistoryFlag, "history", false, "Mostrar historial de asignaciones y sesiones de esfuerzo")
	ticketGetCmd.Flags().BoolVar(&getOfflineFlag, "offline", false, "Consultar únicamente desde la caché local sin llamadas a la red")
	ticketGetCmd.Flags().StringVar(&getIncludeFlag, "include", "", "Secciones a incluir separadas por comas (ej. comments,history)")

	// 4. ticket create
	var createProjectFlag, createPriorityFlag, createStateFlag, createDescFlag string
	var createStartDateFlag, createTargetDateFlag, createLabelsFlag string
	ticketCreateCmd := &cobra.Command{
		Use:   "create <título>",
		Short: "Crea una nueva tarea (en Plane CE o local si está offline)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			var labels []string
			if createLabelsFlag != "" {
				for _, l := range strings.Split(createLabelsFlag, ",") {
					if trimmed := strings.TrimSpace(l); trimmed != "" {
						labels = append(labels, trimmed)
					}
				}
			}

			created, err := svc.Create(ctx, issue.CreateIssueInput{
				ProjectKey:  createProjectFlag,
				Title:       args[0],
				Description: createDescFlag,
				Priority:    createPriorityFlag,
				StateName:   createStateFlag,
				Labels:      labels,
				StartDate:   createStartDateFlag,
				TargetDate:  createTargetDateFlag,
			})
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "CREATE_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
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
	ticketCreateCmd.Flags().StringVar(&createStartDateFlag, "start-date", "", "Fecha de inicio planificada (YYYY-MM-DD)")
	ticketCreateCmd.Flags().StringVar(&createTargetDateFlag, "due-date", "", "Fecha límite / de entrega (YYYY-MM-DD)")
	ticketCreateCmd.Flags().StringVar(&createTargetDateFlag, "target-date", "", "Alias de --due-date (YYYY-MM-DD)")
	ticketCreateCmd.Flags().StringVar(&createLabelsFlag, "labels", "", "Etiquetas separadas por comas (ej. backend,p0,auth)")

	// 5. ticket update
	var updateStateFlag, updatePriorityFlag, updateTitleFlag string
	var updateStartDateFlag, updateTargetDateFlag string
	var updateDescFlag, updateAssignFlag, updateLabelsFlag string
	ticketUpdateCmd := &cobra.Command{
		Use:   "update <TICKET-ID>",
		Short: "Actualiza el estado, prioridad, título, asignado o descripción de una tarea",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

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
			if updateDescFlag != "" {
				input.Description = &updateDescFlag
			}
			if updateAssignFlag != "" {
				input.Assignee = &updateAssignFlag
			}
			if updateLabelsFlag != "" {
				rawLabels := strings.Split(updateLabelsFlag, ",")
				var cleaned []string
				for _, l := range rawLabels {
					trimmed := strings.TrimSpace(l)
					if trimmed != "" {
						cleaned = append(cleaned, trimmed)
					}
				}
				input.Labels = &cleaned
			}
			if updateStartDateFlag != "" {
				input.StartDate = &updateStartDateFlag
			}
			if updateTargetDateFlag != "" {
				input.TargetDate = &updateTargetDateFlag
			}

			updated, err := svc.Update(ctx, args[0], input)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "UPDATE_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
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
	ticketUpdateCmd.Flags().StringVar(&updateDescFlag, "desc", "", "Nueva descripción")
	ticketUpdateCmd.Flags().StringVar(&updateAssignFlag, "assign", "", "Reasignar a usuario (UUID o identificador)")
	ticketUpdateCmd.Flags().StringVar(&updateLabelsFlag, "labels", "", "Etiquetas separadas por coma")
	ticketUpdateCmd.Flags().StringVar(&updateStartDateFlag, "start-date", "", "Fecha de inicio planificada (YYYY-MM-DD)")
	ticketUpdateCmd.Flags().StringVar(&updateTargetDateFlag, "due-date", "", "Fecha límite / de entrega (YYYY-MM-DD)")
	ticketUpdateCmd.Flags().StringVar(&updateTargetDateFlag, "target-date", "", "Alias de --due-date (YYYY-MM-DD)")

	// 6. ticket sync (delegado a SyncUnified)
	var syncAllProjectsFlag bool
	var syncCatalogsFlag bool
	var syncDeepFlag bool
	ticketSyncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Descarga y actualiza proyectos, estados y tareas desde Plane CE (delegado a 'alo sync')",
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			target := "tickets"
			if syncCatalogsFlag {
				target = "catalogs"
			}

			opts := issue.SyncUnifiedOptions{
				Target:      target,
				ProjectKey:  ticketProjectFlag,
				AllProjects: syncAllProjectsFlag,
				Deep:        syncDeepFlag,
			}

			report, err := svc.SyncUnified(ctx, opts)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "SYNC_FAILED",
					Category: errors.CategoryUpstreamService,
					Hint:     "Verifica la conectividad con Plane CE",
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(report, true)
				return
			}

			if syncCatalogsFlag {
				fmt.Printf("🔄 Catálogos sincronizados: %d proyectos, estados y etiquetas actualizados en SQLite.\n", report.SyncedProjects)
				return
			}

			fmt.Printf("🔄 Sincronización completada: %d tareas actualizadas en SQLite.\n", report.SyncedIssues)
			for proj, wm := range report.Watermarks {
				if wm != "" {
					fmt.Printf("   • %s watermark: %s\n", proj, wm)
				}
			}
		},
	}
	ticketSyncCmd.Flags().StringVarP(&ticketProjectFlag, "project", "p", "", "Proyecto específico a sincronizar")
	ticketSyncCmd.Flags().BoolVar(&syncAllProjectsFlag, "all-projects", false, "Sincronizar todos los proyectos")
	ticketSyncCmd.Flags().BoolVarP(&syncCatalogsFlag, "catalogs", "c", false, "Sincronizar únicamente catálogos (proyectos, estados, etiquetas)")
	ticketSyncCmd.Flags().BoolVar(&syncDeepFlag, "deep", false, "Sincronización profunda: ignora marcas de agua y reindexa todo")

	// 7. ticket whoami
	ticketWhoamiCmd := &cobra.Command{
		Use:   "whoami",
		Short: "Verifica la conectividad y credenciales con Plane CE",
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			user, err := svc.Whoami(ctx)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "AUTH_FAILED",
					Category: errors.CategoryAuth,
					Hint:     "Verifica PLANE_API_KEY y PLANE_BASE_URL",
				}, errors.ExitAuthError, jsonFlag)
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

	// 8. ticket labels
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

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

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

	// 9. ticket delete
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

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

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
				title, _ := res["title"].(string)
				comments, _ := res["comments"].(int)
				hoursOwn, _ := res["hours_own"].(int)
				ownMinutes, _ := res["own_minutes"].(int)

				what := fmt.Sprintf("se pierden sus %d comentarios en Plane y %d registro(s) de horas tuyas (%d min)", comments, hoursOwn, ownMinutes)
				fmt.Printf("⚠ Eliminar %s «%s» es irreversible: %s.\n", args[0], title, what)
				fmt.Println("  También se pierden adjuntos, enlaces, subtareas e historial.")
				fmt.Printf("  Para conservarlo sin eliminar: alo ticket update %s --state Cancelado\n", args[0])
				discardHint := ""
				if hoursOwn > 0 {
					discardHint = " --discard-worklogs"
				}
				fmt.Printf("  Para confirmar eliminación:    alo ticket delete %s --yes%s\n", args[0], discardHint)
				return
			}
			hoursDiscarded, _ := res["hours_discarded"].(int)
			discardNotice := ""
			if hoursDiscarded > 0 {
				discardNotice = fmt.Sprintf(" (%d registro(s) de horas descartado(s))", hoursDiscarded)
			}
			fmt.Printf("✓ Tarea %s eliminada limpiamente%s.\n", args[0], discardNotice)
		},
	}
	ticketDeleteCmd.Flags().BoolVar(&ticketDeleteYesFlag, "yes", false, "Confirmar eliminación")
	ticketDeleteCmd.Flags().BoolVar(&ticketDeleteDiscardFlag, "discard-worklogs", false, "Descartar horas registradas asociadas")

	// 10. ticket link
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

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

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

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

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

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

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

	// 11. ticket focus
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

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

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

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

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

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

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

	// 12. ticket projects
	ticketProjectsCmd := &cobra.Command{
		Use:     "projects",
		Aliases: []string{"proj"},
		Short:   "Listar proyectos disponibles en el workspace",
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			projects, err := svc.Projects(ctx)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "PROJECTS_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(projects, true)
				return
			}

			if len(projects) == 0 {
				fmt.Println("No hay proyectos sincronizados. Ejecuta 'alo ticket sync' para descargar el catálogo.")
				return
			}

			fmt.Printf("%-12s %-36s %s\n", "PROYECTO", "ID", "NOMBRE")
			fmt.Println(strings.Repeat("─", 80))
			for _, p := range projects {
				fmt.Printf("%-12s %-36s %s\n", p.Identifier, p.ProjectID, p.Name)
			}
		},
	}

	// 13. ticket states
	var ticketStatesProjectFlag string
	var ticketStatesRefreshFlag bool
	ticketStatesCmd := &cobra.Command{
		Use:     "states",
		Aliases: []string{"st"},
		Short:   "Listar los estados de workflow por proyecto",
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			states, err := svc.States(ctx, ticketStatesProjectFlag, ticketStatesRefreshFlag)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "STATES_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if states == nil {
				states = make([]issue.StateRecord, 0)
			}

			if jsonFlag {
				payload := map[string]interface{}{
					"count":   len(states),
					"results": states,
					"schema":  "allostasis/1",
					"view":    "states",
				}
				errors.EmitSuccess(payload, true)
				return
			}

			if len(states) == 0 {
				fmt.Println("No se encontraron estados registrados. Ejecuta 'alo ticket sync' o usa '--refresh'.")
				return
			}

			fmt.Printf("%-10s %-12s %-22s %-18s %s\n", "PROYECTO", "GRUPO", "ESTADO", "SLUG", "ID")
			fmt.Println(strings.Repeat("─", 88))
			for _, st := range states {
				proj := st.ProjectKey
				if proj == "" {
					proj = st.ProjectID
				}
				defMark := ""
				if st.IsDefault {
					defMark = " *"
				}
				fmt.Printf("%-10s %-12s %-22s %-18s %s\n", proj, st.GroupName, st.Name+defMark, st.Slug, st.ID)
			}
		},
	}
	ticketStatesCmd.Flags().StringVarP(&ticketStatesProjectFlag, "project", "p", "", "Filtrar por proyecto (ej. OMNIFY, OPS)")
	ticketStatesCmd.Flags().BoolVar(&ticketStatesRefreshFlag, "refresh", false, "Refrescar estados desde Plane CE")

	// 14. ticket comment
	ticketCommentCmd := &cobra.Command{
		Use:     "comment",
		Aliases: []string{"cmt", "comments"},
		Short:   "Gestión de comentarios y bitácoras de trabajo sobre una tarea",
	}

	var commentHoursFlag float64
	var commentStartTimeFlag string
	var commentTrackFlag string
	commentAddCmd := &cobra.Command{
		Use:   "add <TICKET-ID> <MENSAJE>",
		Short: "Añade un comentario o bitácora de tiempo a una tarea",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			rec, err := svc.AddComment(ctx, args[0], args[1], commentHoursFlag, commentStartTimeFlag, commentTrackFlag)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "COMMENT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(rec, true)
				return
			}

			if commentHoursFlag > 0 {
				fmt.Printf("✓ Bitácora registrada en %s (%.1f hrs): %s\n", args[0], commentHoursFlag, args[1])
			} else {
				fmt.Printf("✓ Comentario añadido a %s: %s\n", args[0], args[1])
			}
		},
	}
	commentAddCmd.Flags().Float64Var(&commentHoursFlag, "hours", 0, "Horas invertidas a registrar como bitácora")
	commentAddCmd.Flags().StringVar(&commentStartTimeFlag, "start-time", "", "Hora de inicio en formato HH:MM")
	commentAddCmd.Flags().StringVar(&commentTrackFlag, "track", "main", "Track de ejecución (main, etc.)")

	commentEditCmd := &cobra.Command{
		Use:   "edit <TICKET-ID> <COMMENT-ID> <NUEVO-MENSAJE>",
		Short: "Edita el contenido de un comentario en Plane CE",
		Args:  cobra.ExactArgs(3),
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			if err := svc.EditComment(ctx, args[0], args[1], args[2]); err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "EDIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(map[string]interface{}{"status": "updated", "comment_id": args[1]}, true)
				return
			}

			fmt.Printf("✓ Comentario %s actualizado en %s\n", args[1], args[0])
		},
	}

	var commentDeleteYesFlag bool
	commentDeleteCmd := &cobra.Command{
		Use:   "delete <TICKET-ID> <COMMENT-ID>",
		Short: "Elimina un comentario en Plane CE",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			if !commentDeleteYesFlag {
				errors.EmitErrorWithDetail(fmt.Errorf("se requiere confirmación con --yes para eliminar el comentario"), errors.ErrorDetail{
					Code:             "CONFIRMATION_REQUIRED",
					Category:         errors.CategoryUserInput,
					SuggestedCommand: fmt.Sprintf("alo ticket comment delete %s %s --yes", args[0], args[1]),
				}, errors.ExitUsageError, jsonFlag)
			}

			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			if err := svc.DeleteComment(ctx, args[0], args[1]); err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "DELETE_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(map[string]interface{}{"status": "deleted", "comment_id": args[1]}, true)
				return
			}

			fmt.Printf("✓ Comentario %s eliminado de %s\n", args[1], args[0])
		},
	}
	commentDeleteCmd.Flags().BoolVarP(&commentDeleteYesFlag, "yes", "y", false, "Confirmar eliminación definitiva del comentario")

	var commentListLastFlag int
	var commentListOfflineFlag bool
	commentListCmd := &cobra.Command{
		Use:     "list <TICKET-ID>",
		Aliases: []string{"ls"},
		Short:   "Lista los comentarios y bitácoras asociadas a una tarea",
		Args:    cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			comments, err := svc.GetComments(ctx, args[0], commentListLastFlag, commentListOfflineFlag)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "LIST_COMMENTS_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if comments == nil {
				comments = []issue.CommentRecord{}
			}

			if jsonFlag {
				errors.EmitSuccess(comments, true)
				return
			}

			if len(comments) == 0 {
				fmt.Printf("No hay comentarios registrados para %s.\n", args[0])
				return
			}

			fmt.Printf("💬 Comentarios en %s (%d):\n", args[0], len(comments))
			for _, c := range comments {
				author := c.CreatedBy
				if author == "" {
					author = c.Actor
				}
				if author == "" {
					author = "usuario"
				}
				cleanText := stripHTMLTags(c.CommentHTML)
				fmt.Printf("   • [%s] %s: %s\n", c.ID, author, cleanText)
			}
		},
	}
	commentListCmd.Flags().IntVar(&commentListLastFlag, "last", 10, "Número de comentarios recientes a mostrar")
	commentListCmd.Flags().BoolVar(&commentListOfflineFlag, "offline", false, "Leer exclusivamente de la caché local")

	ticketCommentCmd.AddCommand(commentAddCmd, commentEditCmd, commentDeleteCmd, commentListCmd)

	// 15. ticket outbox
	ticketOutboxCmd := &cobra.Command{
		Use:   "outbox",
		Short: "Inspección y gestión de la cola outbox de mutaciones pendientes",
	}

	var outboxListAllFlag bool
	outboxListCmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Lista las mutaciones encoladas en outbox",
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			items, err := svc.ListOutbox(ctx, outboxListAllFlag)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "OUTBOX_LIST_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if items == nil {
				items = []issue.OutboxRecord{}
			}

			if jsonFlag {
				errors.EmitSuccess(items, true)
				return
			}

			if len(items) == 0 {
				fmt.Println("No hay mutaciones pendientes en el outbox.")
				return
			}

			fmt.Printf("%-6s %-12s %-10s %-10s %-10s %s\n", "ID", "TICKET", "TIPO", "ESTADO", "INTENTOS", "CLIENT_ID")
			fmt.Println(strings.Repeat("─", 75))
			for _, it := range items {
				fmt.Printf("%-6d %-12s %-10s %-10s %-10d %s\n", it.ID, it.TicketRef, it.MutationType, it.State, it.Attempts, it.ClientID)
			}
		},
	}
	outboxListCmd.Flags().BoolVarP(&outboxListAllFlag, "all", "a", false, "Incluir mutaciones enviadas o fallidas además de pendientes")

	outboxDropCmd := &cobra.Command{
		Use:   "drop <ID>",
		Short: "Elimina un registro atascado de la cola outbox",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			var id int64
			if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil {
				errors.EmitErrorWithDetail(fmt.Errorf("el ID debe ser un número entero"), errors.ErrorDetail{
					Code:     "INVALID_ARG",
					Category: errors.CategoryUserInput,
				}, errors.ExitUsageError, jsonFlag)
			}

			if err := svc.DropOutbox(ctx, id); err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "DROP_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(map[string]interface{}{"status": "dropped", "id": id}, true)
				return
			}

			fmt.Printf("✓ Mutación %d descartada del outbox.\n", id)
		},
	}

	outboxDLQCmd := &cobra.Command{
		Use:   "dlq",
		Short: "Lista las mutaciones aisladas en Dead Letter Queue (DLQ) tras 5 intentos fallidos",
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			items, err := svc.ListDeadLetterOutbox(ctx)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "DLQ_LIST_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			if items == nil {
				items = []issue.OutboxRecord{}
			}

			if jsonFlag {
				errors.EmitSuccess(items, true)
				return
			}

			if len(items) == 0 {
				fmt.Println("✓ La Dead Letter Queue (DLQ) está vacía: cero mutaciones envenenadas.")
				return
			}

			fmt.Printf("☠️ %d Mutaciones Aisladas en Dead Letter Queue (DLQ):\n", len(items))
			fmt.Printf("%-6s %-12s %-10s %-10s %-10s %s\n", "ID", "TICKET", "TIPO", "ESTADO", "INTENTOS", "ÚLTIMO ERROR")
			fmt.Println(strings.Repeat("─", 80))
			for _, it := range items {
				fmt.Printf("%-6d %-12s %-10s %-10s %-10d %s\n", it.ID, it.TicketRef, it.MutationType, it.State, it.Attempts, it.LastError)
			}
		},
	}

	ticketOutboxCmd.AddCommand(outboxListCmd, outboxDropCmd, outboxDLQCmd)

	ticketCmd.AddCommand(ticketListCmd, ticketSearchCmd, ticketGetCmd, ticketCreateCmd, ticketUpdateCmd, ticketSyncCmd, ticketWhoamiCmd, ticketLabelsCmd, ticketDeleteCmd, ticketLinkCmd, ticketFocusCmd, ticketProjectsCmd, ticketStatesCmd, ticketCommentCmd, ticketOutboxCmd)

	return ticketCmd
}

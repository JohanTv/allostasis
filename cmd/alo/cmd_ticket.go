package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/presentation/errors"
	"github.com/spf13/cobra"
)

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
	var ticketAllProjectsFlag, ticketBriefFlag, ticketMineFlag, ticketOpenFlag bool

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
				filter := issue.IssueFilter{
					ProjectKey: targetProject,
					State:      ticketStateFlag,
					Limit:      ticketLimitFlag,
					OpenOnly:   ticketOpenFlag,
				}
				if ticketMineFlag {
					if actorUUID := os.Getenv("PLANE_ACTOR_UUID"); actorUUID != "" {
						filter.Assignee = actorUUID
					} else if user, whoErr := svc.Whoami(ctx); whoErr == nil && user != nil && user.ID != "" {
						filter.Assignee = user.ID
					}
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

			item, err := svc.Get(ctx, args[0])
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "NOT_FOUND",
					Category: errors.CategoryUserInput,
					Hint:     fmt.Sprintf("Verifica la clave de la tarea: %s", args[0]),
				}, errors.ExitNotFound, jsonFlag)
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

	// 4. ticket create
	var createProjectFlag, createPriorityFlag, createStateFlag, createDescFlag string
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

			created, err := svc.Create(ctx, issue.CreateIssueInput{
				ProjectKey:  createProjectFlag,
				Title:       args[0],
				Description: createDescFlag,
				Priority:    createPriorityFlag,
				StateName:   createStateFlag,
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

	// 5. ticket update
	var updateStateFlag, updatePriorityFlag, updateTitleFlag string
	ticketUpdateCmd := &cobra.Command{
		Use:   "update <TICKET-ID>",
		Short: "Actualiza el estado, prioridad o título de una tarea",
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

	// 6. ticket sync
	var syncAllProjectsFlag bool
	ticketSyncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Descarga y actualiza proyectos, estados y tareas desde Plane CE",
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

			count, err := svc.Sync(ctx, ticketProjectFlag, syncAllProjectsFlag)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "SYNC_FAILED",
					Category: errors.CategoryUpstreamService,
					Hint:     "Verifica la conectividad con Plane CE",
				}, errors.ExitGeneralError, jsonFlag)
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
				fmt.Printf("⚠ Alcance de borrado para %s:\n  Para confirmar la eliminación permanente incluye: --yes\n", args[0])
				return
			}
			fmt.Printf("✓ Tarea %s eliminada limpiamente.\n", args[0])
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

	ticketCmd.AddCommand(ticketListCmd, ticketSearchCmd, ticketGetCmd, ticketCreateCmd, ticketUpdateCmd, ticketSyncCmd, ticketWhoamiCmd, ticketLabelsCmd, ticketDeleteCmd, ticketLinkCmd, ticketFocusCmd)

	return ticketCmd
}

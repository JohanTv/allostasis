package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/presentation/errors"
	"github.com/spf13/cobra"
)

func newSyncCmd() *cobra.Command {
	var projectFlag string
	var allProjectsFlag bool
	var deepFlag bool
	var limitFlag int

	cmd := &cobra.Command{
		Use:   "sync [target]",
		Short: "Sincronización bidireccional, delta pull de tareas y catálogos con Plane CE",
		Long: `Sincroniza de forma soberana el estado local de Allostasis con Plane CE (ADR 0028 / RFC 0027).

Targets disponibles:
  all       (default) Sincronización bidireccional: drena outbox, actualiza catálogos y delta pull de tareas
  tickets   Delta pull incremental de tareas (usa --deep para ignorar marcas de agua)
  catalogs  Sincronización rápida de proyectos, estados de workflow y etiquetas
  outbox    Drenaje exclusivo de eventos diferidos y bitácoras locales hacia Plane
  hours     Conciliación de registros de tiempo y horas contra comentarios upstream`,
		Args: cobra.MaximumNArgs(1),
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

			target := "all"
			if len(args) > 0 {
				target = strings.ToLower(strings.TrimSpace(args[0]))
			}

			opts := issue.SyncUnifiedOptions{
				Target:      target,
				ProjectKey:  projectFlag,
				AllProjects: allProjectsFlag,
				Deep:        deepFlag,
				Limit:       limitFlag,
			}

			report, err := svc.SyncUnified(ctx, opts)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "SYNC_FAILED",
					Category: errors.CategoryUpstreamService,
					Hint:     "Verifica la conectividad con Plane CE o la validez de PLANE_API_KEY",
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(report, true)
				return
			}

			fmt.Printf("🔄 [SINCRONIZACIÓN PLANE CE] - Target: %s | Estado: %s\n", report.Target, report.Status)
			if report.SentComments > 0 || report.FailedComments > 0 {
				fmt.Printf("   • Outbox Comentarios:  %d enviados, %d fallidos/reintentos\n", report.SentComments, report.FailedComments)
			}
			if report.SyncedCatalogs {
				fmt.Printf("   • Catálogos:           %d proyectos, estados y etiquetas sincronizados\n", report.SyncedProjects)
			}
			if report.SyncedIssues > 0 || len(report.Watermarks) > 0 {
				fmt.Printf("   • Tareas Actualizadas: %d tareas descargadas en caché\n", report.SyncedIssues)
				for proj, wm := range report.Watermarks {
					if wm != "" {
						fmt.Printf("     - %s watermark: %s\n", proj, wm)
					}
				}
			}
			if len(report.SkippedProjects) > 0 {
				names := make([]string, 0, len(report.SkippedProjects))
				for _, sp := range report.SkippedProjects {
					names = append(names, sp.Project)
				}
				fmt.Printf("   • Proyectos omitidos:  %s (sin permiso)\n", strings.Join(names, ", "))
			}
			if report.DurationMs > 0 {
				fmt.Printf("   • Duración:            %d ms\n", report.DurationMs)
			}
			for _, e := range report.Errors {
				fmt.Printf("   ⚠ %s\n", e)
			}
		},
	}

	cmd.Flags().StringVarP(&projectFlag, "project", "p", "", "Proyecto específico a sincronizar (ej. OMNIFY, OPS)")
	cmd.Flags().BoolVar(&allProjectsFlag, "all-projects", false, "Iterar sobre todos los proyectos del workspace")
	cmd.Flags().BoolVar(&deepFlag, "deep", false, "Sincronización profunda: ignora marcas de agua y reindexa todo")
	cmd.Flags().IntVar(&limitFlag, "limit", 100, "Límite máximo de eventos outbox o tareas por lote")

	return cmd
}

package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/allostasis/alo/internal/domain/ledger"
	"github.com/allostasis/alo/internal/presentation/errors"
	"github.com/allostasis/alo/internal/presentation/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newAgendaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "agenda",
		Short: "Muestra el estado alostático del día y focos activos",
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, _, err := initCockpitService()
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

			snap, err := svc.GetSnapshot(ctx, ".worktrees")
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "SNAPSHOT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
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
}

func newLoadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "load",
		Short: "Calcula y audita el desgaste por soporte invisible (Toil Ratio)",
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, _, err := initCockpitService()
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

			snap, err := svc.GetSnapshot(ctx, ".worktrees")
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "SNAPSHOT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
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
}

func newLogCmd() *cobra.Command {
	var (
		bucketFlag      string
		interactiveFlag bool
	)

	cmd := &cobra.Command{
		Use:   "log [TICKET-ID] [DURACION_MINUTOS] [NOTAS...]",
		Short: "Registra una sesión de esfuerzo en el ledger alostático",
		Args:  cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, _, err := initCockpitService()
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
				errors.EmitErrorWithDetail(fmt.Errorf("uso requerido: alo log <TICKET-ID> <MINUTOS> [NOTAS...] o 'alo log -i'"), errors.ErrorDetail{
					Code:             "INVALID_USAGE",
					Category:         errors.CategoryUserInput,
					SuggestedCommand: "alo log ALO-10 45 \"Desarrollo de feature\"",
				}, errors.ExitUsageError, jsonFlag)
			}

			ticketID := strings.ToUpper(args[0])
			durationMins, err := strconv.Atoi(args[1])
			if err != nil || durationMins <= 0 {
				errors.EmitErrorWithDetail(fmt.Errorf("la duración debe ser un número entero positivo de minutos"), errors.ErrorDetail{
					Code:     "INVALID_DURATION",
					Category: errors.CategoryUserInput,
				}, errors.ExitUsageError, jsonFlag)
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
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "LOG_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
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

	cmd.Flags().StringVar(&bucketFlag, "bucket", "deep_work", "Bucket de capacidad (deep_work, toil_support, coordination, standby)")
	cmd.Flags().BoolVarP(&interactiveFlag, "interactive", "i", false, "Lanzar formulario interactivo con Charm Huh")

	return cmd
}

package main

import (
	"context"
	"fmt"

	"github.com/allostasis/alo/internal/presentation/errors"
	"github.com/spf13/cobra"
)

func newSyncCmd() *cobra.Command {
	var limitFlag int

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sincroniza eventos diferidos (outbox) y comentarios con Plane CE",
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

			report, err := svc.FlushOutbox(ctx, limitFlag)
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

			fmt.Printf("🔄 [SINCRONIZACIÓN PLANE CE] - Estado: %s\n", report.Status)
			fmt.Printf("   • Comentarios publicados: %d\n", report.SentComments)
			fmt.Printf("   • Fallidos / Reintentos:  %d\n", report.FailedComments)
			for _, e := range report.Errors {
				fmt.Printf("   ⚠ %s\n", e)
			}
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 20, "Límite máximo de eventos outbox a procesar por lote")
	return cmd
}

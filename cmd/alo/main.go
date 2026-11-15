package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/allostasis/alo/internal/presentation/errors"
)

func main() {
	// Captura limpia de señales del sistema operativo (Graceful Shutdown)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rootCmd := newRootCmd()

	// Blindaje Machine-to-Machine y ejecución segura ante panics
	errors.SafeExecute(jsonFlag, func() {
		if err := rootCmd.ExecuteContext(ctx); err != nil {
			if ctx.Err() != nil {
				errors.EmitErrorWithDetail(ctx.Err(), errors.ErrorDetail{
					Code:        "OPERATION_CANCELED",
					Category:    errors.CategoryInternal,
					Hint:        "Operación interrumpida por señal de terminación del sistema (SIGINT/SIGTERM)",
					Recoverable: true,
				}, errors.ExitGeneralError, jsonFlag)
			}
			os.Exit(1)
		}
	})
}

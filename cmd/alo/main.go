package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/JohanTv/allostasis/internal/presentation/errors"
)

func main() {
	// Captura limpia de señales del sistema operativo (Graceful Shutdown)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	requestedJSON := false
	for _, arg := range os.Args[1:] {
		if arg == "--json" {
			requestedJSON = true
			break
		}
	}

	rootCmd := newRootCmd()

	// Blindaje Machine-to-Machine y ejecución segura ante panics
	errors.SafeExecute(requestedJSON, func() {
		if err := rootCmd.ExecuteContext(ctx); err != nil {
			if ctx.Err() != nil {
				errors.EmitErrorWithDetail(ctx.Err(), errors.ErrorDetail{
					Code:        "OPERATION_CANCELED",
					Category:    errors.CategoryInternal,
					Hint:        "Operación interrumpida por señal de terminación del sistema (SIGINT/SIGTERM)",
					Recoverable: true,
				}, errors.ExitGeneralError, requestedJSON || jsonFlag)
				return
			}
			if requestedJSON || jsonFlag {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:        "COMMAND_FAILED",
					Category:    errors.CategoryInternal,
					Hint:        "Verifica los argumentos y flags invocados con 'alo --help'",
					Recoverable: true,
				}, errors.ExitGeneralError, true)
				return
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	})
}

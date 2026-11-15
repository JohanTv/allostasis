package errors

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
)

type ErrorCategory string

const (
	CategoryUserInput       ErrorCategory = "user_input"
	CategoryStateConflict   ErrorCategory = "state_conflict"
	CategoryAuth            ErrorCategory = "auth"
	CategoryUpstreamService ErrorCategory = "upstream_service"
	CategoryInternal        ErrorCategory = "internal"
)

const (
	ExitSuccess        = 0
	ExitGeneralError   = 1
	ExitUsageError     = 2
	ExitAuthError      = 3
	ExitNotFound       = 4
	ExitTransientError = 5
	ExitSoftwareError  = 70 // EX_SOFTWARE
)

// ErrorDetail estructura el error de forma legible para humanos y procesable por agentes de IA.
type ErrorDetail struct {
	Code             string                 `json:"code"`
	Category         ErrorCategory          `json:"category"`
	Message          string                 `json:"message"`
	Target           string                 `json:"target,omitempty"`
	Hint             string                 `json:"hint,omitempty"`
	SuggestedCommand string                 `json:"suggested_command,omitempty"`
	Recoverable      bool                   `json:"recoverable"`
	Context          map[string]interface{} `json:"context,omitempty"`
}

// ResponseEnvelope representa la envoltura canónica de salida en modo --json.
type ResponseEnvelope struct {
	OK    bool         `json:"ok"`
	Error *ErrorDetail `json:"error,omitempty"`
	Data  interface{}  `json:"data,omitempty"`
}

// EmitError emite el error garantizando que los agentes de IA reciban JSON estricto por stdout sin secuencias ANSI.
func EmitError(err error, code string, hint string, jsonMode bool) {
	EmitErrorWithDetail(err, ErrorDetail{
		Code:     code,
		Hint:     hint,
		Category: CategoryUserInput,
	}, ExitGeneralError, jsonMode)
}

// EmitErrorWithDetail emite un error con atributos semánticos completos.
func EmitErrorWithDetail(err error, detail ErrorDetail, exitCode int, jsonMode bool) {
	if exitCode == 0 {
		exitCode = ExitGeneralError
	}
	if detail.Code == "" {
		detail.Code = "EXECUTION_ERROR"
	}
	if detail.Category == "" {
		detail.Category = CategoryUserInput
	}
	if detail.Message == "" && err != nil {
		detail.Message = err.Error()
	}

	if jsonMode {
		env := ResponseEnvelope{
			OK:    false,
			Error: &detail,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(env)
		os.Exit(exitCode)
	}

	// Modo texto legible para humanos por stderr
	fmt.Fprintf(os.Stderr, "✖ Error [%s]: %s\n", detail.Code, detail.Message)
	if detail.Target != "" {
		fmt.Fprintf(os.Stderr, "  Campo/Parámetro afectado: %s\n", detail.Target)
	}
	if detail.Hint != "" {
		fmt.Fprintf(os.Stderr, "💡 Sugerencia: %s\n", detail.Hint)
	}
	if detail.SuggestedCommand != "" {
		fmt.Fprintf(os.Stderr, "👉 Ejecuta: %s\n", detail.SuggestedCommand)
	}
	os.Exit(exitCode)
}

// EmitSuccess emite una respuesta exitosa envuelta en JSON por stdout si jsonMode está activo.
func EmitSuccess(data interface{}, jsonMode bool) {
	if jsonMode {
		env := ResponseEnvelope{
			OK:   true,
			Data: data,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(env)
		return
	}
}

// SafeExecute encapsula la ejecución de un CLI protegiendo contra panics no controlados.
func SafeExecute(jsonMode bool, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			detail := ErrorDetail{
				Code:        "INTERNAL_PANIC",
				Category:    CategoryInternal,
				Message:     fmt.Sprintf("Pánico no controlado en la aplicación: %v", r),
				Hint:        "Este es un error interno del software. Por favor reporta este incidente adjuntando el payload.",
				Recoverable: false,
				Context: map[string]interface{}{
					"panic": fmt.Sprint(r),
					"stack": stack,
				},
			}

			if jsonMode {
				env := ResponseEnvelope{
					OK:    false,
					Error: &detail,
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				_ = enc.Encode(env)
				os.Exit(ExitSoftwareError)
			}

			fmt.Fprintf(os.Stderr, "\n💥 FATAL: Pánico no controlado en la aplicación: %v\n", r)
			fmt.Fprintf(os.Stderr, "Detalles técnicos (stack trace):\n%s\n", stack)
			os.Exit(ExitSoftwareError)
		}
	}()

	fn()
}

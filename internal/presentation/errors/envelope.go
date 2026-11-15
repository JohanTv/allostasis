package errors

import (
	"encoding/json"
	"fmt"
	"os"
)

// ErrorDetail estructura el error de forma legible para humanos y procesable por agentes de IA.
type ErrorDetail struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Hint        string `json:"hint,omitempty"`
	Recoverable bool   `json:"recoverable"`
}

// ResponseEnvelope representa la envoltura canónica de salida en modo --json.
type ResponseEnvelope struct {
	OK    bool         `json:"ok"`
	Error *ErrorDetail `json:"error,omitempty"`
	Data  interface{}  `json:"data,omitempty"`
}

// EmitError emite el error garantizando que los agentes de IA reciban JSON estricto sin secuencias ANSI.
func EmitError(err error, code string, hint string, jsonMode bool) {
	if code == "" {
		code = "EXECUTION_ERROR"
	}

	detail := &ErrorDetail{
		Code:        code,
		Message:     err.Error(),
		Hint:        hint,
		Recoverable: true,
	}

	if jsonMode {
		env := ResponseEnvelope{
			OK:    false,
			Error: detail,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(env)
		os.Exit(1)
	}

	// Modo texto legible para humanos
	fmt.Fprintf(os.Stderr, "✖ Error [%s]: %s\n", code, err.Error())
	if hint != "" {
		fmt.Fprintf(os.Stderr, "💡 Sugerencia: %s\n", hint)
	}
	os.Exit(1)
}

// EmitSuccess emite una respuesta exitosa envuelta en JSON si jsonMode está activo.
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

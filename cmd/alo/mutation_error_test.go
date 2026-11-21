package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/JohanTv/allostasis/internal/domain/issue"
	presentation "github.com/JohanTv/allostasis/internal/presentation/errors"
)

func TestMutationErrorDetail(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		category presentation.ErrorCategory
		exit     int
		hintHas  string
	}{
		{"etiqueta desconocida", &issue.UnknownLabelError{Names: []string{"x"}, Valid: []string{"a"}}, presentation.CategoryUserInput, presentation.ExitUsageError, "alo ticket labels"},
		{"rechazo 400 de Plane", &issue.RejectedError{Status: 400, Operation: "crear tarea"}, presentation.CategoryUserInput, presentation.ExitUsageError, "corrígelos"},
		{"estado desconocido", &issue.UnknownStateError{Name: "x", Valid: []string{"a"}}, presentation.CategoryUserInput, presentation.ExitUsageError, "alo ticket states"},
		{"adjunto inválido con pista propia", &issue.InvalidAttachmentError{Path: "a.sql", Reason: "r", Hint: "renombra a .txt"}, presentation.CategoryUserInput, presentation.ExitUsageError, "renombra a .txt"},
		{"precondición con pista propia", &issue.PreconditionError{Message: "m", Hint: "ejecuta alo sync"}, presentation.CategoryUserInput, presentation.ExitUsageError, "alo sync"},
		{"credenciales", &issue.AuthError{Status: 401}, presentation.CategoryAuth, presentation.ExitAuthError, "whoami"},
		{"transitorio", &issue.TransientError{Status: 503}, presentation.CategoryUpstreamService, presentation.ExitTransientError, "reintenta"},
		// Lo que nadie clasificó cae en "internal"/1: se muestra, nunca se silencia.
		{"fallo genérico", errors.New("boom"), presentation.CategoryInternal, presentation.ExitGeneralError, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			detail, exit := mutationErrorDetail(tc.err, "CREATE_FAILED")
			if detail.Category != tc.category || exit != tc.exit || detail.Code != "CREATE_FAILED" {
				t.Errorf("detail=%+v exit=%d, esperado categoría %s exit %d", detail, exit, tc.category, tc.exit)
			}
			if tc.hintHas != "" && !strings.Contains(detail.Hint, tc.hintHas) {
				t.Errorf("hint %q no contiene %q", detail.Hint, tc.hintHas)
			}
		})
	}
}

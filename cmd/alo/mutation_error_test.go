package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/allostasis/alo/internal/domain/issue"
	presentation "github.com/allostasis/alo/internal/presentation/errors"
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

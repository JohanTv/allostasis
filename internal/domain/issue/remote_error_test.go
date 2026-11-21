package issue_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/JohanTv/allostasis/internal/domain/issue"
)

func TestClassifyRemoteError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want issue.RemoteErrorKind
	}{
		{"nil", nil, issue.RemoteErrUnknown},
		{"etiqueta desconocida", &issue.UnknownLabelError{Names: []string{"x"}}, issue.RemoteErrRejected},
		{"adjunto inválido", &issue.InvalidAttachmentError{Path: "a.sql"}, issue.RemoteErrRejected},
		{"rechazo 400", &issue.RejectedError{Status: 400}, issue.RemoteErrRejected},
		{"auth 401", &issue.AuthError{Status: 401}, issue.RemoteErrAuth},
		{"transitorio 503", &issue.TransientError{Status: 503}, issue.RemoteErrTransient},
		{"transitorio de red", &issue.TransientError{Cause: errors.New("connection refused")}, issue.RemoteErrTransient},
		{"envuelto con %w", fmt.Errorf("ctx: %w", &issue.TransientError{Status: 502}), issue.RemoteErrTransient},
		// Default-deny: un error que nadie clasificó nunca se trata como reintentable ni como ignorable.
		{"sin clasificar", errors.New("HTTP 500 Internal Server Error"), issue.RemoteErrUnknown},
	}
	for _, tc := range cases {
		if got := issue.ClassifyRemoteError(tc.err); got != tc.want {
			t.Errorf("%s: clase = %s, esperado %s", tc.name, got, tc.want)
		}
	}
}

func TestIsInputError_OnlyRejected(t *testing.T) {
	if !issue.IsInputError(&issue.RejectedError{Status: 422}) || !issue.IsInputError(&issue.UnknownLabelError{}) {
		t.Error("un rechazo de datos debe ser error de entrada")
	}
	if issue.IsInputError(&issue.TransientError{Status: 500}) || issue.IsInputError(&issue.AuthError{Status: 401}) || issue.IsInputError(errors.New("x")) {
		t.Error("transitorio, auth y desconocido no son errores de entrada")
	}
}

func TestValidateAttachmentFile(t *testing.T) {
	if err := issue.ValidateAttachmentFile("nota.txt", 10); err != nil {
		t.Errorf("archivo válido rechazado: %v", err)
	}
	if err := issue.ValidateAttachmentFile("dump.SQL", 10); err == nil || !strings.Contains(err.Error(), ".sql") {
		t.Errorf("la extensión .sql (sin distinguir mayúsculas) debe rechazarse, obtuvo %v", err)
	}
	if err := issue.ValidateAttachmentFile("grande.bin", issue.MaxAttachmentBytes+1); err == nil {
		t.Error("más de 50 MB debe rechazarse")
	}
	if err := issue.ValidateAttachmentFile("justo.bin", issue.MaxAttachmentBytes); err != nil {
		t.Errorf("exactamente 50 MB es válido: %v", err)
	}
}

func TestValidateAttachmentID(t *testing.T) {
	if err := issue.ValidateAttachmentID("11111111-2222-3333-4444-555555555555"); err != nil {
		t.Errorf("UUID válido rechazado: %v", err)
	}
	for _, bad := range []string{"", "abc", "1111-2222", "gggggggg-2222-3333-4444-555555555555"} {
		if err := issue.ValidateAttachmentID(bad); err == nil {
			t.Errorf("%q debe rechazarse", bad)
		}
	}
}

func TestAttachmentContentType(t *testing.T) {
	if got := issue.AttachmentContentType("nota.txt"); got != "text/plain" {
		t.Errorf("txt = %q, esperado text/plain (sin parámetros)", got)
	}
	if got := issue.AttachmentContentType("archivo.desconocidoxyz"); got != "application/octet-stream" {
		t.Errorf("desconocido = %q", got)
	}
}

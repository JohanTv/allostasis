package tracker_plane_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/allostasis/alo/internal/adapters/tracker_plane"
	"github.com/allostasis/alo/internal/domain/issue"
)

// FetchIssuesDelta debe tipar 401/403 como AuthError (el sync lo usa para omitir un proyecto sin permiso)
// y dejar el resto de fallos (p. ej. 429) con el error genérico de siempre.
func TestFetchIssuesDelta_ClassifiesAuthStatusesAndKeepsOthers(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		wantAuth bool
	}{
		{"403 sin permiso sobre el proyecto", http.StatusForbidden, true},
		{"401 credenciales inválidas", http.StatusUnauthorized, true},
		{"429 rate limit sigue siendo genérico", http.StatusTooManyRequests, false},
		{"500 sigue siendo genérico", http.StatusInternalServerError, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"detail":"rechazado"}`))
			}))
			defer srv.Close()

			c := tracker_plane.NewClient(tracker_plane.Config{BaseURL: srv.URL, APIKey: "k", Workspace: "ws", Timeout: 2 * time.Second})
			_, _, err := c.FetchIssuesDelta(context.Background(), "proj-1", "OMNIFY", 100, "", false)
			if err == nil {
				t.Fatal("se esperaba un error")
			}
			var authErr *issue.AuthError
			gotAuth := errors.As(err, &authErr)
			if gotAuth != tc.wantAuth {
				t.Fatalf("AuthError=%v, se esperaba %v (err=%v)", gotAuth, tc.wantAuth, err)
			}
			if gotAuth && authErr.Status != tc.status {
				t.Errorf("status del AuthError %d, se esperaba %d", authErr.Status, tc.status)
			}
			if !tc.wantAuth && !strings.Contains(err.Error(), "al listar tareas") {
				t.Errorf("el texto del error genérico cambió: %v", err)
			}
		})
	}
}

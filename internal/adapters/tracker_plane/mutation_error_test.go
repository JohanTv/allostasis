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

// Un 4xx por datos inválidos es un rechazo (no reintentable); auth y 5xx son fallos transitorios.
func TestClient_MutationErrors_RejectedVsTransient(t *testing.T) {
	cases := []struct {
		status       int
		wantRejected bool
	}{
		{http.StatusBadRequest, true},
		{http.StatusNotFound, true},
		{http.StatusConflict, true},
		{http.StatusUnprocessableEntity, true},
		{http.StatusUnauthorized, false},
		{http.StatusTooManyRequests, false},
		{http.StatusInternalServerError, false},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"error":"x"}`))
		}))
		client := tracker_plane.NewClient(tracker_plane.Config{BaseURL: srv.URL, APIKey: "k", Workspace: "ws", Timeout: 2 * time.Second})
		ctx := context.Background()

		_, createErr := client.CreateIssueRemote(ctx, "p", issue.CreateIssueInput{ProjectKey: "OMNIFY", Title: "t", Priority: "medium"}, "")
		title := "t"
		updateErr := client.UpdateIssueRemote(ctx, "p", "i", issue.UpdateIssueInput{Title: &title}, "")
		srv.Close()

		for op, err := range map[string]error{"create": createErr, "update": updateErr} {
			var rejected *issue.RejectedError
			if got := errors.As(err, &rejected); got != tc.wantRejected {
				t.Errorf("HTTP %d (%s): RejectedError = %v, esperado %v (err: %v)", tc.status, op, got, tc.wantRejected, err)
			}
			if !strings.Contains(err.Error(), "HTTP") {
				t.Errorf("el mensaje debe conservar el código HTTP: %v", err)
			}
		}
	}
}

package tracker_plane_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/allostasis/alo/internal/adapters/tracker_plane"
)

func fetchProjectsFrom(t *testing.T, body string) map[string]*bool {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := tracker_plane.NewClient(tracker_plane.Config{BaseURL: srv.URL, APIKey: "k", Workspace: "ws", Timeout: 2 * time.Second})
	recs, err := c.FetchProjects(context.Background())
	if err != nil {
		t.Fatalf("FetchProjects falló: %v", err)
	}
	out := make(map[string]*bool, len(recs))
	for _, r := range recs {
		out[r.Identifier] = r.IsMember
	}
	return out
}

// Plane informa is_member:true en los proyectos del usuario y OMITE el campo en los públicos donde no es miembro.
func TestFetchProjects_MembershipFromIsMemberField(t *testing.T) {
	got := fetchProjectsFrom(t, `{"results":[
		{"id":"p1","identifier":"omnify","name":"Omnify","is_member":true,"member_role":15},
		{"id":"p2","identifier":"omnileads","name":"Omnileads","network":2},
		{"id":"p3","identifier":"ops","name":"Ops","is_member":false}
	]}`)
	if got["OMNIFY"] == nil || !*got["OMNIFY"] {
		t.Errorf("OMNIFY debe ser miembro: %v", got["OMNIFY"])
	}
	if got["OMNILEADS"] == nil || *got["OMNILEADS"] {
		t.Errorf("sin el campo (mientras otros lo traen) debe ser NO miembro: %v", got["OMNILEADS"])
	}
	if got["OPS"] == nil || *got["OPS"] {
		t.Errorf("is_member:false debe ser NO miembro: %v", got["OPS"])
	}
}

// Si NINGÚN proyecto trae el campo (otra versión de Plane) la pertenencia es desconocida: nada se excluye.
func TestFetchProjects_MembershipUnknownWhenPlaneDoesNotReportIt(t *testing.T) {
	got := fetchProjectsFrom(t, `[
		{"id":"p1","identifier":"omnify","name":"Omnify"},
		{"id":"p2","identifier":"ops","name":"Ops"}
	]`)
	for id, m := range got {
		if m != nil {
			t.Errorf("%s: sin el campo en ningún proyecto debe quedar desconocido (nil), obtuvo %v", id, *m)
		}
	}
}

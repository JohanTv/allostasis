package main_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/JohanTv/allostasis/internal/adapters/sqlite"
)

// TestCLI_TicketSync_CatalogsContract verifica de forma hermética el contrato de `ticket sync --catalogs --json`
// contra un Plane simulado: sin red, sin credenciales reales y sobre una base SQLite temporal.
func TestCLI_TicketSync_CatalogsContract(t *testing.T) {
	const projects = "/api/v1/workspaces/ws/projects/"

	var (
		mu   sync.Mutex
		hits []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.URL.Path)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case projects:
			// Plane marca `is_member: true` solo en los proyectos del usuario y omite el campo en los públicos.
			_, _ = w.Write([]byte(`{"results":[
				{"id":"proj-member","identifier":"MEM","name":"Member Project","is_member":true},
				{"id":"proj-public","identifier":"PUB","name":"Public Project"}
			]}`))
		case projects + "proj-member/states/":
			_, _ = w.Write([]byte(`{"results":[{"id":"st-mem","name":"Progreso","slug":"progreso","group":"started"}]}`))
		case projects + "proj-member/labels/":
			_, _ = w.Write([]byte(`{"results":[{"id":"lbl-mem","name":"backend","color":"#ff0000"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	e := newE2EEnv(t, srv.URL)
	run := e.run("ticket", "sync", "--catalogs", "--json")
	if run.exit != 0 {
		t.Fatalf("exit=%d, stdout=%s, stderr=%s", run.exit, run.stdout, run.stderr)
	}
	if run.stderr != "" {
		t.Errorf("stderr debe estar vacío bajo --json: %s", run.stderr)
	}

	ok, data, errObj := run.envelope(t)
	if !ok || errObj != nil {
		t.Fatalf("envelope no exitoso: data=%v error=%v", data, errObj)
	}
	if data["status"] != "catalogs_synced" {
		t.Errorf("status esperado 'catalogs_synced', obtenido %v (errors: %v)", data["status"], data["errors"])
	}
	if data["synced_projects"] != float64(2) || data["synced_catalogs"] != true {
		t.Errorf("reporte de catálogos inesperado: %+v", data)
	}

	// Solo se descargan estados y etiquetas de los proyectos donde el usuario es miembro.
	mu.Lock()
	defer mu.Unlock()
	for _, path := range hits {
		if strings.Contains(path, "proj-public/") {
			t.Errorf("no se debe consultar el catálogo de un proyecto sin membresía: %s", path)
		}
	}

	store, err := sqlite.NewStore(e.db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	states, err := store.ListStates(ctx, "proj-member")
	if err != nil || len(states) != 1 || states[0].ID != "st-mem" {
		t.Errorf("estados persistidos inesperados: %+v (err: %v)", states, err)
	}
	labels, err := store.ListLabels(ctx, "proj-member")
	if err != nil || len(labels) != 1 || labels[0].ID != "lbl-mem" {
		t.Errorf("etiquetas persistidas inesperadas: %+v (err: %v)", labels, err)
	}
	cachedProjects, err := store.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, p := range cachedProjects {
		seen[p.Identifier] = true
	}
	if !seen["MEM"] || !seen["PUB"] {
		t.Errorf("la base local debe reflejar ambos proyectos del workspace: %+v", cachedProjects)
	}
}

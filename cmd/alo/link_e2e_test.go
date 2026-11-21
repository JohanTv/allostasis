package main_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JohanTv/allostasis/internal/adapters/sqlite"
	"github.com/JohanTv/allostasis/internal/domain/issue"
)

type e2eLinkPlane struct {
	mu         sync.Mutex
	links      map[string]map[string]any
	status     int
	apiCalls   int
}

func newE2ELinkPlane(t *testing.T) (*e2eLinkPlane, *httptest.Server) {
	t.Helper()
	p := &e2eLinkPlane{links: map[string]map[string]any{}}
	const base = "/api/v1/workspaces/ws/projects/proj-1/issues/iss-2/links/"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		if r.URL.Path == base && r.Method == http.MethodGet {
			p.apiCalls++
			if p.status != 0 {
				w.WriteHeader(p.status)
				_, _ = w.Write([]byte(`{"detail":"error configurado"}`))
				return
			}
			var items []map[string]any
			for _, item := range p.links {
				items = append(items, item)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"results": items})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return p, srv
}

func TestCLI_Link_List_EndToEnd(t *testing.T) {
	plane, srv := newE2ELinkPlane(t)
	plane.links["lnk-1"] = map[string]any{
		"id":         "lnk-1",
		"title":      "PR Activa",
		"url":        "https://github.com/org/repo/pull/1",
		"created_at": "2026-11-21T12:00:00Z",
	}
	e := newE2EEnv(t, srv.URL)

	// Sembrar un enlace viejo en SQLite para probar reconciliación
	store, err := sqlite.NewStore(e.db)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.AddLink(context.Background(), issue.Link{
		ID:        "lnk-stale",
		TicketID:  "OMNIFY-2",
		IssueID:   "iss-2",
		Title:     "PR Antigua Eliminada",
		URL:       "https://github.com/org/repo/pull/99",
		CreatedAt: time.Now(),
	})
	store.Close()

	// 1. Plane responde: JSON contract
	run := e.run("ticket", "link", "list", "OMNIFY-2", "--json")
	if run.exit != 0 {
		t.Fatalf("exit=%d, stderr=%s", run.exit, run.stderr)
	}
	if run.stderr != "" {
		t.Errorf("stderr debe estar vacío bajo --json: %s", run.stderr)
	}
	ok, data, errObj := run.envelope(t)
	if !ok || errObj != nil {
		t.Fatalf("envelope no exitoso: %v, %v", data, errObj)
	}
	if data["source"] != "api" || data["complete"] != true || data["ticket"] != "OMNIFY-2" {
		t.Errorf("data inesperada: %+v", data)
	}
	linksList, ok := data["links"].([]any)
	if !ok || len(linksList) != 1 {
		t.Fatalf("links inesperados: %+v", data["links"])
	}
	first := linksList[0].(map[string]any)
	if first["id"] != "lnk-1" {
		t.Errorf("id inesperado: %v", first["id"])
	}

	// Verificar que el enlace viejo fue eliminado localmente (reconciliación)
	store, _ = sqlite.NewStore(e.db)
	cached, _ := store.ListLinks(context.Background(), "OMNIFY-2")
	store.Close()
	if len(cached) != 1 || cached[0].ID != "lnk-1" {
		t.Errorf("la base local debe reflejar solo los enlaces vigentes de Plane: %+v", cached)
	}

	// 2. Modo texto: legible y con la fuente declarada
	run = e.run("ticket", "link", "list", "OMNIFY-2")
	if run.exit != 0 || !strings.Contains(run.stdout, "[fuente: api]") {
		t.Errorf("salida de texto inesperada: %q", run.stdout)
	}
	if !strings.Contains(run.stdout, "PR Activa") {
		t.Errorf("debe contener el título del enlace: %q", run.stdout)
	}

	// 3. Fallo transitorio (503): devuelve la copia local con [fuente: cache]
	plane.mu.Lock()
	plane.status = http.StatusServiceUnavailable
	plane.mu.Unlock()

	run = e.run("ticket", "link", "list", "OMNIFY-2", "--json")
	if run.exit != 0 {
		t.Fatalf("fallo transitorio no debe terminar con error en list: exit=%d", run.exit)
	}
	ok, data, _ = run.envelope(t)
	if !ok || data["source"] != "cache" || data["complete"] != false {
		t.Errorf("debe marcar source=cache y complete=false: %+v", data)
	}
	if data["note"] == "" || !strings.Contains(data["note"].(string), "Plane no respondió") {
		t.Errorf("debe incluir note explicativa: %v", data["note"])
	}

	// Modo texto en fallback de caché
	run = e.run("ticket", "link", "list", "OMNIFY-2")
	if run.exit != 0 || !strings.Contains(run.stdout, "[fuente: cache]") {
		t.Errorf("salida de texto inesperada en fallback: %q", run.stdout)
	}
	if !strings.Contains(run.stdout, "⚠ Plane no respondió") {
		t.Errorf("debe mostrar aviso ⚠: %q", run.stdout)
	}

	// 4. Credenciales rechazadas (401): falla en lugar de disfrazarse de caché
	plane.mu.Lock()
	plane.status = http.StatusUnauthorized
	plane.mu.Unlock()

	run = e.run("ticket", "link", "list", "OMNIFY-2", "--json")
	ok, _, errObj = run.envelope(t)
	if ok || run.exit != 3 || errObj["category"] != "auth" {
		t.Fatalf("exit=%d (esperado 3), errObj=%v", run.exit, errObj)
	}
	if run.stderr != "" {
		t.Errorf("bajo --json stderr debe ser 0 bytes: %s", run.stderr)
	}
}

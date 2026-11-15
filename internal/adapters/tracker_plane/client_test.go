package tracker_plane_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/allostasis/alo/internal/adapters/tracker_plane"
	_ "github.com/ncruces/go-sqlite3/driver"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", "file:plane_test_mem?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("error abriendo db: %v", err)
	}
	db.SetMaxOpenConns(1)

	schema := `
	CREATE TABLE IF NOT EXISTS issues_cache (
		id TEXT PRIMARY KEY,
		ticket_id TEXT NOT NULL UNIQUE,
		project_id TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS outbox (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		client_id TEXT NOT NULL,
		ticket_ref TEXT NOT NULL,
		payload_json TEXT NOT NULL,
		target_workspace TEXT,
		state TEXT NOT NULL DEFAULT 'pending',
		attempts INTEGER NOT NULL DEFAULT 0,
		last_error TEXT,
		plane_comment_id TEXT,
		sent_at TEXT
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("error creando schema: %v", err)
	}

	// Insertar datos de prueba
	_, _ = db.Exec(`INSERT OR REPLACE INTO issues_cache(id, ticket_id, project_id) VALUES('issue-uuid-1', 'ALO-10', 'proj-uuid-1')`)
	_, _ = db.Exec(`INSERT INTO outbox(client_id, ticket_ref, payload_json, target_workspace, state) VALUES('c-1', 'ALO-10', '{"comment_html":"<p>test</p>"}', 'teamprogra', 'pending')`)

	return db
}

func TestClient_FlushOutbox_Success(t *testing.T) {
	// 1. Simular servidor Plane CE con httptest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "secret-token-123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"comment-plane-777"}`))
	}))
	defer server.Close()

	db := setupTestDB(t)
	defer db.Close()

	cfg := tracker_plane.Config{
		BaseURL:   server.URL,
		APIKey:    "secret-token-123",
		Workspace: "teamprogra",
		Timeout:   2 * time.Second,
	}
	client := tracker_plane.NewClient(cfg)

	ctx := context.Background()
	report, err := client.FlushOutbox(ctx, db, 10)
	if err != nil {
		t.Fatalf("FlushOutbox falló: %v", err)
	}

	if report.SentComments != 1 {
		t.Errorf("se esperaba 1 comentario publicado, obtenidos %d", report.SentComments)
	}

	// Verificar actualización en la base de datos
	var state, commentID string
	err = db.QueryRow(`SELECT state, plane_comment_id FROM outbox WHERE client_id = 'c-1'`).Scan(&state, &commentID)
	if err != nil {
		t.Fatalf("error consultando outbox: %v", err)
	}
	if state != "sent" || commentID != "comment-plane-777" {
		t.Errorf("estado o commentID inesperado: state=%s, commentID=%s", state, commentID)
	}
}

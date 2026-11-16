package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/app"
	"github.com/allostasis/alo/internal/domain/issue"
)

func TestTrackerService_CRUD_LocalFirst(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error abriendo store: %v", err)
	}
	defer store.Close()

	svc := app.NewTrackerService(store, nil)

	// 1. Crear tarea local
	created, err := svc.Create(ctx, issue.CreateIssueInput{
		ProjectKey:  "OMNIFY",
		Title:       "Implementar módulo de búsqueda en Go",
		Description: "Soporte FTS para títulos y descripciones de tickets",
		Priority:    "high",
		StateName:   "Pendiente",
	})
	if err != nil {
		t.Fatalf("Create falló: %v", err)
	}
	if created.Title != "Implementar módulo de búsqueda en Go" {
		t.Errorf("título inesperado: %s", created.Title)
	}

	// 2. Obtener por Key
	item, err := svc.Get(ctx, created.Key)
	if err != nil {
		t.Fatalf("Get falló: %v", err)
	}
	if item.Priority != "high" {
		t.Errorf("prioridad inesperada: %s", item.Priority)
	}

	// 3. Buscar por texto
	results, err := svc.Search(ctx, "módulo de búsqueda", "OMNIFY", 5)
	if err != nil {
		t.Fatalf("Search falló: %v", err)
	}
	if len(results) != 1 || results[0].Key != created.Key {
		t.Fatalf("se esperaba encontrar la tarea creada por búsqueda, obtuvo: %+v", results)
	}

	// 4. Actualizar estado
	newTitle := "Módulo de búsqueda en Go completado"
	newState := "Progreso"
	updated, err := svc.Update(ctx, created.Key, issue.UpdateIssueInput{
		Title:     &newTitle,
		StateName: &newState,
	})
	if err != nil {
		t.Fatalf("Update falló: %v", err)
	}
	if updated.Title != newTitle || updated.StateName != newState {
		t.Errorf("actualización no reflejada: %+v", updated)
	}
}

type mockTrackerClient struct {
	sentComments map[string]string
	comments     map[string][]issue.CommentRecord
}

func (m *mockTrackerClient) Whoami(ctx context.Context) (*issue.UserInfo, error) {
	return &issue.UserInfo{ID: "usr-1", Email: "test@example.com"}, nil
}
func (m *mockTrackerClient) FetchProjects(ctx context.Context) ([]issue.ProjectRecord, error) {
	return []issue.ProjectRecord{{Identifier: "OMNIFY", ProjectID: "proj-1", Name: "Omnify"}}, nil
}
func (m *mockTrackerClient) FetchStates(ctx context.Context, projectID string) ([]issue.StateRecord, error) {
	return []issue.StateRecord{{ID: "st-1", ProjectID: projectID, Name: "Backlog", Slug: "backlog", GroupName: "backlog"}}, nil
}
func (m *mockTrackerClient) FetchLabels(ctx context.Context, projectID string) ([]issue.Label, error) {
	return []issue.Label{{ID: "lbl-1", ProjectID: projectID, Name: "backend"}}, nil
}
func (m *mockTrackerClient) FetchIssues(ctx context.Context, projectID, projectKey string, limit int) ([]issue.IssueSnapshot, error) {
	return []issue.IssueSnapshot{
		{
			Item: issue.WorkItem{ID: "iss-1", Key: "OMNIFY-10", ProjectID: projectID, Title: "Remote task"},
			RawJSON: `{"id":"iss-1","title":"Remote task"}`,
		},
	}, nil
}
func (m *mockTrackerClient) CreateIssueRemote(ctx context.Context, projectID string, input issue.CreateIssueInput, stateID string) (*issue.WorkItem, error) {
	return &issue.WorkItem{ID: "iss-remote-1", Key: input.ProjectKey + "-100", Title: input.Title}, nil
}
func (m *mockTrackerClient) UpdateIssueRemote(ctx context.Context, projectID, issueID string, input issue.UpdateIssueInput, stateID string) error {
	return nil
}
func (m *mockTrackerClient) DeleteIssueRemote(ctx context.Context, projectID, issueID string) error {
	return nil
}
func (m *mockTrackerClient) FetchComments(ctx context.Context, projectID, issueID string) ([]issue.CommentRecord, error) {
	if m.comments != nil {
		return m.comments[issueID], nil
	}
	return nil, nil
}
func (m *mockTrackerClient) SendCommentRaw(ctx context.Context, targetWS, projectID, issueID, payloadJSON string) (string, error) {
	if m.sentComments == nil {
		m.sentComments = make(map[string]string)
	}
	m.sentComments[issueID] = payloadJSON
	return "comment-remote-999", nil
}
func (m *mockTrackerClient) EditCommentRemote(ctx context.Context, projectID, issueID, commentID, newBody string) error {
	return nil
}
func (m *mockTrackerClient) DeleteCommentRemote(ctx context.Context, projectID, issueID, commentID string) error {
	return nil
}
func (m *mockTrackerClient) AddLinkRemote(ctx context.Context, projectID, issueID, title, linkURL string) (*issue.Link, error) {
	return &issue.Link{ID: "lnk-1", IssueID: issueID, Title: title, URL: linkURL}, nil
}
func (m *mockTrackerClient) ListLinksRemote(ctx context.Context, projectID, issueID string) ([]issue.Link, error) {
	return nil, nil
}
func (m *mockTrackerClient) DeleteLinkRemote(ctx context.Context, projectID, issueID, linkID string) error {
	return nil
}

func TestTrackerService_FlushOutbox(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error abriendo store: %v", err)
	}
	defer store.Close()

	// Guardar issue para que exista en el store
	task := &issue.WorkItem{
		ID:        "iss-uuid-42",
		Key:       "OMNIFY-42",
		ProjectID: "proj-omnify",
		Title:     "Ticket para outbox",
	}
	if err := store.SaveIssue(ctx, task); err != nil {
		t.Fatalf("error guardando issue: %v", err)
	}

	// Encolar outbox item
	if err := store.EnqueueOutbox(ctx, "client-1", "OMNIFY-42", `{"comment_html":"<p>Bitácora de cierre</p>"}`, "test-workspace"); err != nil {
		t.Fatalf("error encolando outbox: %v", err)
	}

	mockClient := &mockTrackerClient{}
	svc := app.NewTrackerService(store, mockClient)

	report, err := svc.FlushOutbox(ctx, 10)
	if err != nil {
		t.Fatalf("FlushOutbox falló: %v", err)
	}
	if report.SentComments != 1 {
		t.Errorf("se esperaba 1 comentario enviado, obtenidos %d", report.SentComments)
	}
	if mockClient.sentComments["iss-uuid-42"] != `{"comment_html":"<p>Bitácora de cierre</p>"}` {
		t.Errorf("payload enviado inesperado: %v", mockClient.sentComments)
	}

	// Verificar que el registro en outbox se actualizó a 'sent'
	pending, err := store.ListOutbox(ctx, false)
	if err != nil {
		t.Fatalf("error consultando outbox: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("se esperaba 0 registros pendientes, obtenidos %d", len(pending))
	}
}

func TestTrackerService_Delete_Safeguards(t *testing.T) {
	t.Setenv("PLANE_ACTOR_UUID", "usr-me")
	ctx := context.Background()
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error abriendo store: %v", err)
	}
	defer store.Close()

	// Guardar perfil de usuario para tener identidad propia
	if err := store.SaveUserProfile(ctx, issue.UserInfo{ID: "usr-me", Email: "me@example.com"}); err != nil {
		t.Fatalf("error guardando perfil: %v", err)
	}

	task := &issue.WorkItem{
		ID:        "iss-100",
		Key:       "OMNIFY-100",
		ProjectID: "proj-1",
		Title:     "Tarea de prueba delete",
	}
	if err := store.SaveIssue(ctx, task); err != nil {
		t.Fatalf("error guardando issue: %v", err)
	}

	mockClient := &mockTrackerClient{
		comments: make(map[string][]issue.CommentRecord),
	}
	svc := app.NewTrackerService(store, mockClient)

	// 1. Rechazo por pending outbox
	if err := store.EnqueueOutbox(ctx, "c-outbox", "OMNIFY-100", `{"comment_html":"test"}`, "test-workspace"); err != nil {
		t.Fatalf("error encolando outbox: %v", err)
	}
	_, err = svc.Delete(ctx, "OMNIFY-100", true, false)
	if err == nil || !strings.Contains(err.Error(), "cola de salida") {
		t.Fatalf("se esperaba rechazo por outbox pendiente, obtenido: %v", err)
	}
	// Limpiar outbox
	outboxList, _ := store.ListOutbox(ctx, true)
	for _, o := range outboxList {
		_ = store.DropOutboxItem(ctx, o.ID)
	}

	// 2. Rechazo incondicional por horas de terceros (ADR 0018)
	mockClient.comments["iss-100"] = []issue.CommentRecord{
		{
			ID:          "comm-foreign",
			CommentHTML: "<p>Bitácora</p><!-- PLANE_WORKLOG_METADATA {\"duration_minutes\": 60} -->",
			Actor:       "usr-other",
			CreatedBy:   "usr-other",
		},
	}
	_, err = svc.Delete(ctx, "OMNIFY-100", true, true)
	if err == nil || !strings.Contains(err.Error(), "horas de otra persona") {
		t.Fatalf("se esperaba rechazo incondicional por horas foráneas, obtenido: %v", err)
	}

	// 3. Rechazo de horas propias sin --discard-worklogs
	mockClient.comments["iss-100"] = []issue.CommentRecord{
		{
			ID:          "comm-own",
			CommentHTML: "<p>Bitácora propia</p><!-- PLANE_WORKLOG_METADATA {\"duration_minutes\": 45} -->",
			Actor:       "usr-me",
			CreatedBy:   "usr-me",
		},
	}
	_, err = svc.Delete(ctx, "OMNIFY-100", true, false)
	if err == nil || !strings.Contains(err.Error(), "--discard-worklogs") {
		t.Fatalf("se esperaba rechazo exigiendo --discard-worklogs, obtenido: %v", err)
	}

	// 4. Modo dry-run cuando falta --yes
	res, err := svc.Delete(ctx, "OMNIFY-100", false, true)
	if err != nil {
		t.Fatalf("dry-run falló: %v", err)
	}
	if res["status"] != "dry_run" {
		t.Fatalf("se esperaba status dry_run, obtenido: %v", res["status"])
	}

	// 5. Borrado exitoso con --yes y --discard-worklogs
	res, err = svc.Delete(ctx, "OMNIFY-100", true, true)
	if err != nil {
		t.Fatalf("borrado confirmado falló: %v", err)
	}
	if res["status"] != "deleted" {
		t.Fatalf("se esperaba status deleted, obtenido: %v", res["status"])
	}

	// Verificar que el ticket ya no existe localmente
	deletedItem, err := store.GetIssue(ctx, "OMNIFY-100")
	if err == nil && deletedItem != nil {
		t.Fatalf("el ticket debería haberse eliminado localmente")
	}
}

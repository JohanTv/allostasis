package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/app"
	"github.com/allostasis/alo/internal/domain/clock"
	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/domain/ledger"
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
			Item:    issue.WorkItem{ID: "iss-1", Key: "OMNIFY-10", ProjectID: projectID, Title: "Remote task"},
			RawJSON: `{"id":"iss-1","title":"Remote task"}`,
		},
	}, nil
}
func (m *mockTrackerClient) FetchIssuesDelta(ctx context.Context, projectID, projectKey string, limit int, cutoffISO string, deep bool) ([]issue.IssueSnapshot, string, error) {
	snaps, err := m.FetchIssues(ctx, projectID, projectKey, limit)
	newest := ""
	if len(snaps) > 0 && snaps[0].Item.RemoteUpdatedAt != nil {
		newest = snaps[0].Item.RemoteUpdatedAt.Format(time.RFC3339)
	}
	return snaps, newest, err
}
func (m *mockTrackerClient) CreateIssueRemote(ctx context.Context, projectID string, input issue.CreateIssueInput, stateID string) (*issue.WorkItem, error) {
	return &issue.WorkItem{ID: "iss-remote-1", Key: input.ProjectKey + "-100", Title: input.Title}, nil
}
func (m *mockTrackerClient) UpdateIssueRemote(ctx context.Context, projectID, issueID string, input issue.UpdateIssueInput, stateID string) error {
	return nil
}
func (m *mockTrackerClient) UploadAttachmentRemote(ctx context.Context, projectID, issueID string, up issue.AttachmentUpload) (*issue.Attachment, error) {
	return &issue.Attachment{ID: "att-remote-1", IssueID: issueID, Name: up.Name, SizeBytes: up.Size}, nil
}
func (m *mockTrackerClient) ListAttachmentsRemote(ctx context.Context, projectID, issueID string) ([]issue.Attachment, bool, error) {
	return nil, true, nil
}
func (m *mockTrackerClient) DeleteAttachmentRemote(ctx context.Context, projectID, issueID, attachmentID string) error {
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

func TestTrackerService_Comments_And_History(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error abriendo store: %v", err)
	}
	defer store.Close()

	// Crear issue existente
	item := &issue.WorkItem{
		ID:        "iss-remote-555",
		Key:       "OMNIFY-555",
		ProjectID: "proj-omnify",
		Title:     "Investigar latencia en sync",
		State:     issue.StateStarted,
		StateName: "En Progreso",
		Priority:  "high",
		CreatedBy: "usr-johan",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.SaveIssue(ctx, item); err != nil {
		t.Fatalf("error guardando issue: %v", err)
	}

	mockClient := &mockTrackerClient{
		comments: map[string][]issue.CommentRecord{
			"iss-remote-555": {
				{
					ID:          "cmt-1",
					CommentHTML: "<p>Comentario inicial remoto</p>",
					Actor:       "usr-alice",
				},
			},
		},
	}
	svc := app.NewTrackerService(store, mockClient)

	// 1. Añadir comentario sin horas (online)
	cmt1, err := svc.AddComment(ctx, "OMNIFY-555", "Comentario de prueba", 0, "", "")
	if err != nil {
		t.Fatalf("error AddComment: %v", err)
	}
	if cmt1.ID != "comment-remote-999" {
		t.Errorf("ID retornado inesperado: %s", cmt1.ID)
	}
	sentPayload := mockClient.sentComments["iss-remote-555"]
	if !strings.Contains(sentPayload, "Comentario de prueba") {
		t.Errorf("payload enviado a Plane inesperado: %s", sentPayload)
	}

	// 2. Añadir bitácora con horas
	cmt2, err := svc.AddComment(ctx, "OMNIFY-555", "Sesión de profiling y benchmarking", 1.5, "10:00", "main")
	if err != nil {
		t.Fatalf("error AddComment con horas: %v", err)
	}
	if !strings.Contains(cmt2.CommentHTML, "PLANE_WORKLOG_METADATA") {
		t.Errorf("se esperaba bloque de metadatos en comentario: %s", cmt2.CommentHTML)
	}
	if !strings.Contains(cmt2.CommentHTML, `"duration_minutes":90`) {
		t.Errorf("se esperaban 90 minutos en metadatos: %s", cmt2.CommentHTML)
	}

	// Verificar que el worklog también se persistió localmente
	worklogs, err := store.GetTicketWorklogs(ctx, "OMNIFY-555")
	if err != nil || len(worklogs) == 0 {
		t.Fatalf("no se persistió el worklog localmente: %v (len=%d)", err, len(worklogs))
	}
	if worklogs[0].DurationMinutes != 90 {
		t.Errorf("duración inesperada en worklog local: %d", worklogs[0].DurationMinutes)
	}

	// 3. GetComments (online y offline)
	onlineComments, err := svc.GetComments(ctx, "OMNIFY-555", 10, false)
	if err != nil {
		t.Fatalf("error GetComments online: %v", err)
	}
	if len(onlineComments) == 0 {
		t.Fatalf("se esperaban comentarios remotos cacheados")
	}

	offlineComments, err := svc.GetComments(ctx, "OMNIFY-555", 1, true)
	if err != nil {
		t.Fatalf("error GetComments offline: %v", err)
	}
	if len(offlineComments) != 1 {
		t.Errorf("se esperaba 1 comentario con limit=1, obtenidos: %d", len(offlineComments))
	}

	// 4. GetHistory
	if err := store.RecordAssignmentEvent(ctx, "OMNIFY-555", "usr-johan"); err != nil {
		t.Fatalf("error RecordAssignmentEvent: %v", err)
	}
	hist, err := svc.GetHistory(ctx, "OMNIFY-555")
	if err != nil {
		t.Fatalf("error GetHistory: %v", err)
	}
	if len(hist.Assignments) != 1 {
		t.Errorf("se esperaba 1 asignación en historial, obtenidos: %d", len(hist.Assignments))
	}
	if len(hist.Worklogs) != 1 {
		t.Errorf("se esperaba 1 worklog en historial, obtenidos: %d", len(hist.Worklogs))
	}

	// 5. Update con asignación y etiquetas (las etiquetas deben existir en el catálogo del proyecto)
	for _, name := range []string{"performance", "database"} {
		if err := store.SaveLabel(ctx, issue.Label{ID: "lbl-" + name, ProjectID: "proj-omnify", Name: name}); err != nil {
			t.Fatalf("error guardando etiqueta: %v", err)
		}
	}
	newAssignee := "usr-alice"
	newLabels := []string{"performance", "database"}
	newDesc := "Descripción actualizada"
	updated, err := svc.Update(ctx, "OMNIFY-555", issue.UpdateIssueInput{
		Assignee:    &newAssignee,
		Labels:      &newLabels,
		Description: &newDesc,
	})
	if err != nil {
		t.Fatalf("error Update: %v", err)
	}
	if updated.Assignee != "usr-alice" || updated.Description != "Descripción actualizada" {
		t.Errorf("campos no actualizados correctamente: %+v", updated)
	}
	if len(updated.Labels) != 2 || updated.Labels[0] != "performance" {
		t.Errorf("etiquetas no actualizadas: %+v", updated.Labels)
	}
}

func TestCockpitService_LogTime_Outbox(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error abriendo store: %v", err)
	}
	defer store.Close()

	clk := clock.NewFrozenClock(time.Date(2026, 11, 16, 17, 30, 0, 0, time.UTC))
	svc := app.NewCockpitService(store, store, nil, clk)

	// Registrar tiempo con alo log
	err = svc.LogTime(ctx, "OMNIFY-777", 45, ledger.BucketDeepWork, "Optimizando consultas SQL")
	if err != nil {
		t.Fatalf("error LogTime: %v", err)
	}

	// 1. Verificar worklog en SQLite
	intervals, err := store.GetIntervals(ctx)
	if err != nil || len(intervals) != 1 {
		t.Fatalf("se esperaba 1 intervalo en ledger, obtenidos: %d (%v)", len(intervals), err)
	}
	if intervals[0].Minutes() != 45 {
		t.Errorf("duración esperada 45 min, obtenida: %d", intervals[0].Minutes())
	}

	// 2. Verificar que se encoló la bitácora transaccional en outbox
	outboxItems, err := store.ListOutbox(ctx, true)
	if err != nil || len(outboxItems) != 1 {
		t.Fatalf("se esperaba 1 item en outbox, obtenidos: %d (%v)", len(outboxItems), err)
	}
	item := outboxItems[0]
	if item.TicketRef != "OMNIFY-777" {
		t.Errorf("ticketRef incorrecto en outbox: %s", item.TicketRef)
	}
	if item.MutationType != "comment" {
		t.Errorf("mutationType esperado 'comment', obtenido: %s", item.MutationType)
	}
	if !strings.Contains(item.PayloadJSON, "PLANE_WORKLOG_METADATA") {
		t.Errorf("se esperaba metadata de Plane en payload del outbox: %s", item.PayloadJSON)
	}
	if !strings.Contains(item.PayloadJSON, "duration_minutes") || !strings.Contains(item.PayloadJSON, "45") {
		t.Errorf("se esperaban 45 minutos en payload del outbox: %s", item.PayloadJSON)
	}
}

func TestTrackerService_SyncUnified_TargetsAndWatermarks(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error abriendo store: %v", err)
	}
	defer store.Close()

	now := time.Now()
	mockClient := &mockTrackerClient{
		sentComments: make(map[string]string),
	}
	svc := app.NewTrackerService(store, mockClient)

	// Encolar un comentario en outbox
	item := &issue.WorkItem{
		ID:        "iss-1",
		Key:       "OMNIFY-10",
		ProjectID: "proj-1",
		Title:     "Remote task",
		CreatedAt: now,
		UpdatedAt: now,
	}
	_ = store.SaveIssue(ctx, item)
	_ = store.EnqueueOutboxWithMutation(ctx, "c-1", "OMNIFY-10", `{"comment_html":"<p>test</p>"}`, "test-ws", "comment")

	// 1. Target "outbox": solo drena outbox
	outboxRep, err := svc.SyncUnified(ctx, issue.SyncUnifiedOptions{Target: "outbox"})
	if err != nil {
		t.Fatalf("error SyncUnified outbox: %v", err)
	}
	if outboxRep.SentComments != 1 || outboxRep.SyncedIssues != 0 {
		t.Errorf("resultado outbox inesperado: %+v", outboxRep)
	}

	// 2. Target "catalogs": solo sincroniza catálogos
	catRep, err := svc.SyncUnified(ctx, issue.SyncUnifiedOptions{Target: "catalogs"})
	if err != nil {
		t.Fatalf("error SyncUnified catalogs: %v", err)
	}
	if !catRep.SyncedCatalogs || catRep.SyncedProjects != 1 || catRep.SyncedIssues != 0 {
		t.Errorf("resultado catalogs inesperado: %+v", catRep)
	}

	// 3. Target "tickets": descarga tareas y guarda marca de agua
	tickRep, err := svc.SyncUnified(ctx, issue.SyncUnifiedOptions{
		Target:     "tickets",
		ProjectKey: "OMNIFY",
	})
	if err != nil {
		t.Fatalf("error SyncUnified tickets: %v", err)
	}
	if tickRep.SyncedIssues != 1 {
		t.Errorf("se esperaba 1 tarea sincronizada, obtenidos: %d", tickRep.SyncedIssues)
	}

	// Verificar marca de agua en SQLite
	wm, err := store.GetWatermark(ctx, "issues", "proj-1")
	if err != nil || wm == nil {
		t.Fatalf("error recuperando marca de agua tras sync: %v", err)
	}
	if wm.ItemsSynced != 1 {
		t.Errorf("ItemsSynced esperado 1 en marca de agua, obtenido: %d", wm.ItemsSynced)
	}

	// 4. Target "all": ciclo completo
	_ = store.EnqueueOutboxWithMutation(ctx, "c-2", "OMNIFY-10", `{"comment_html":"<p>test 2</p>"}`, "test-ws", "comment")
	allRep, err := svc.SyncUnified(ctx, issue.SyncUnifiedOptions{Target: "all"})
	if err != nil {
		t.Fatalf("error SyncUnified all: %v", err)
	}
	if allRep.SentComments != 1 || !allRep.SyncedCatalogs || allRep.SyncedIssues != 1 {
		t.Errorf("resultado all inesperado: %+v", allRep)
	}
}

func TestTrackerService_DeterministicClock(t *testing.T) {
	ctx := context.Background()
	frozenTime := time.Date(2026, 11, 21, 15, 30, 0, 0, time.UTC)
	frozenClock := clock.NewFrozenClock(frozenTime)

	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error abriendo store: %v", err)
	}
	defer store.Close()

	_ = store.SaveProject(ctx, issue.ProjectRecord{Identifier: "OMNIFY", ProjectID: "proj-1", Name: "Omnify"})
	svc := app.NewTrackerService(store, nil, frozenClock)

	// 1. Create offline
	item, err := svc.Create(ctx, issue.CreateIssueInput{
		ProjectKey: "OMNIFY",
		Title:      "Tarea con reloj determinista",
	})
	if err != nil {
		t.Fatalf("error creando tarea: %v", err)
	}
	if !item.CreatedAt.Equal(frozenTime) || !item.UpdatedAt.Equal(frozenTime) {
		t.Errorf("fechas no deterministas: created=%v, updated=%v, want=%v", item.CreatedAt, item.UpdatedAt, frozenTime)
	}

	// 2. Update completion
	doneState := "completed"
	_ = store.SaveState(ctx, issue.StateRecord{ID: "st-done", ProjectID: "proj-1", Name: "Completado", GroupName: "completed"})
	updated, err := svc.Update(ctx, item.Key, issue.UpdateIssueInput{
		StateName: &doneState,
	})
	if err != nil {
		t.Fatalf("error actualizando tarea: %v", err)
	}
	if updated.CompletedAt == nil || !updated.CompletedAt.Equal(frozenTime) {
		t.Errorf("CompletedAt no determinista: %v, want=%v", updated.CompletedAt, frozenTime)
	}
}

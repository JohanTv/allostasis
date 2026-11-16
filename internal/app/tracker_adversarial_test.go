package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/app"
	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/domain/ledger"
)

// mockAdversarialTrackerClient emula un tracker upstream (Plane CE) con interruptor de conectividad,
// registro de llamadas y control determinista de respuestas.
type mockAdversarialTrackerClient struct {
	mu           sync.RWMutex
	isOnline     bool
	failAttempts int64
	currAttempts int64

	createdIssues map[string]*issue.WorkItem
	sentComments  map[string][]string // issueID -> []payloadJSON
	seqCounter    int
}

func newMockAdversarialClient(online bool) *mockAdversarialTrackerClient {
	return &mockAdversarialTrackerClient{
		isOnline:      online,
		createdIssues: make(map[string]*issue.WorkItem),
		sentComments:  make(map[string][]string),
		seqCounter:    100,
	}
}

func (m *mockAdversarialTrackerClient) SetOnline(online bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isOnline = online
}

func (m *mockAdversarialTrackerClient) SetFailAttempts(fails int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failAttempts = fails
	m.currAttempts = 0
}

func (m *mockAdversarialTrackerClient) Whoami(ctx context.Context) (*issue.UserInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.isOnline {
		return nil, fmt.Errorf("error de red: dial tcp plane:8000: connect: connection refused")
	}
	return &issue.UserInfo{ID: "usr-adv-1", Email: "adversarial@allostasis.dev", DisplayName: "adv-bot"}, nil
}

func (m *mockAdversarialTrackerClient) FetchProjects(ctx context.Context) ([]issue.ProjectRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.isOnline {
		return nil, fmt.Errorf("error de red: dial tcp plane:8000: connection refused")
	}
	return []issue.ProjectRecord{
		{Identifier: "OMNIFY", ProjectID: "proj-uuid-omnify", Name: "Omnify Core", Workspace: "teamprogra"},
		{Identifier: "OPS", ProjectID: "proj-uuid-ops", Name: "Operations", Workspace: "teamprogra"},
	}, nil
}

func (m *mockAdversarialTrackerClient) FetchStates(ctx context.Context, projectID string) ([]issue.StateRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.isOnline {
		return nil, fmt.Errorf("error de red: timeout")
	}
	return []issue.StateRecord{
		{ID: "st-backlog-" + projectID, ProjectID: projectID, Name: "Backlog", Slug: "backlog", GroupName: "backlog"},
		{ID: "st-progreso-" + projectID, ProjectID: projectID, Name: "Progreso", Slug: "progreso", GroupName: "started"},
	}, nil
}

func (m *mockAdversarialTrackerClient) FetchLabels(ctx context.Context, projectID string) ([]issue.Label, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.isOnline {
		return nil, fmt.Errorf("error de red: timeout")
	}
	return []issue.Label{
		{ID: "lbl-1", ProjectID: projectID, Name: "backend"},
	}, nil
}

func (m *mockAdversarialTrackerClient) FetchIssues(ctx context.Context, projectID, projectKey string, limit int) ([]issue.IssueSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.isOnline {
		return nil, fmt.Errorf("error de red: timeout")
	}
	return []issue.IssueSnapshot{}, nil
}

func (m *mockAdversarialTrackerClient) CreateIssueRemote(ctx context.Context, projectID string, input issue.CreateIssueInput, stateID string) (*issue.WorkItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isOnline {
		return nil, fmt.Errorf("Plane CE offline (HTTP 503 / connection refused)")
	}
	if m.failAttempts > 0 && m.currAttempts < m.failAttempts {
		m.currAttempts++
		return nil, fmt.Errorf("Plane CE sobrecargado temporalmente (intento %d/%d fallido)", m.currAttempts, m.failAttempts)
	}

	m.seqCounter++
	seq := m.seqCounter
	remoteUUID := fmt.Sprintf("iss-plane-uuid-%d", seq)
	remoteKey := fmt.Sprintf("%s-%d", input.ProjectKey, seq)

	created := &issue.WorkItem{
		ID:          remoteUUID,
		Key:         remoteKey,
		ProjectID:   projectID,
		ProjectKey:  input.ProjectKey,
		SequenceID:  seq,
		Title:       input.Title,
		Description: input.Description,
		StateName:   input.StateName,
		Priority:    input.Priority,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	m.createdIssues[remoteKey] = created
	return created, nil
}

func (m *mockAdversarialTrackerClient) UpdateIssueRemote(ctx context.Context, projectID, issueID string, input issue.UpdateIssueInput, stateID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.isOnline {
		return fmt.Errorf("Plane CE offline")
	}
	return nil
}

func (m *mockAdversarialTrackerClient) DeleteIssueRemote(ctx context.Context, projectID, issueID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.isOnline {
		return fmt.Errorf("Plane CE offline")
	}
	return nil
}

func (m *mockAdversarialTrackerClient) SendCommentRaw(ctx context.Context, targetWS, projectID, issueID, payloadJSON string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isOnline {
		return "", fmt.Errorf("Plane CE offline: no se pudo entregar comentario")
	}
	if m.failAttempts > 0 && m.currAttempts < m.failAttempts {
		m.currAttempts++
		return "", fmt.Errorf("Plane CE error transitorio en comentarios (HTTP 502)")
	}

	// Verificar que el issueID no sea un ID local huérfano
	if strings.HasPrefix(issueID, "local-") {
		return "", fmt.Errorf("ERROR FATAL: SendCommentRaw recibió issueID local no reconciliado: %s", issueID)
	}

	m.sentComments[issueID] = append(m.sentComments[issueID], payloadJSON)
	commentUUID := fmt.Sprintf("comment-plane-%d", time.Now().UnixNano())
	return commentUUID, nil
}

func (m *mockAdversarialTrackerClient) EditCommentRemote(ctx context.Context, projectID, issueID, commentID, newBody string) error {
	return nil
}

func (m *mockAdversarialTrackerClient) DeleteCommentRemote(ctx context.Context, projectID, issueID, commentID string) error {
	return nil
}

func (m *mockAdversarialTrackerClient) AddLinkRemote(ctx context.Context, projectID, issueID, title, linkURL string) (*issue.Link, error) {
	return &issue.Link{ID: "lnk-remote-1", IssueID: issueID, Title: title, URL: linkURL}, nil
}

func (m *mockAdversarialTrackerClient) ListLinksRemote(ctx context.Context, projectID, issueID string) ([]issue.Link, error) {
	return nil, nil
}

func (m *mockAdversarialTrackerClient) DeleteLinkRemote(ctx context.Context, projectID, issueID, linkID string) error {
	return nil
}

// TestAdversarial_OfflineOutboxReconciliation_FullCycle prueba el ciclo de vida completo
// de operaciones local-first cuando el tracker remoto está desconectado:
// 1. Creación offline de múltiples tareas en proyectos distintos.
// 2. Registro offline de comentarios y worklogs asociados a los tickets locales provisionales.
// 3. Intento fallido de FlushOutbox en modo desconectado (verificación de no-destrucción de la cola).
// 4. Restauración de conectividad upstream y ejecución de FlushOutbox.
// 5. Verificación de reconciliación canónica: migración de IDs y claves locales a Plane CE,
//    actualización de worklogs dependientes, entrega de comentarios a UUIDs oficiales y limpieza de la outbox.
func TestAdversarial_OfflineOutboxReconciliation_FullCycle(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error abriendo SQLite en memoria: %v", err)
	}
	defer store.Close()

	// Inicializar proyectos y estados locales previamente cacheados
	_ = store.SaveProject(ctx, issue.ProjectRecord{Identifier: "OMNIFY", ProjectID: "proj-uuid-omnify", Name: "Omnify"})
	_ = store.SaveProject(ctx, issue.ProjectRecord{Identifier: "OPS", ProjectID: "proj-uuid-ops", Name: "Operations"})
	_ = store.SaveState(ctx, issue.StateRecord{ID: "st-backlog-omnify", ProjectID: "proj-uuid-omnify", Name: "Backlog", GroupName: "backlog"})
	_ = store.SaveState(ctx, issue.StateRecord{ID: "st-progreso-omnify", ProjectID: "proj-uuid-omnify", Name: "Progreso", GroupName: "started"})
	_ = store.SaveState(ctx, issue.StateRecord{ID: "st-backlog-ops", ProjectID: "proj-uuid-ops", Name: "Backlog", GroupName: "backlog"})

	// FASE 1: CLIENTE TOTALMENTE OFFLINE
	mockClient := newMockAdversarialClient(false)
	svc := app.NewTrackerService(store, mockClient)

	// 1. Crear 3 tareas localmente mientras estamos offline
	task1, err := svc.Create(ctx, issue.CreateIssueInput{
		ProjectKey:  "OMNIFY",
		Title:       "Arquitectura hexagonal resiliente",
		Description: "Desacoplar almacenamiento y transporte",
		Priority:    "urgent",
		StateName:   "Progreso",
	})
	if err != nil {
		t.Fatalf("Create offline task1 falló: %v", err)
	}

	task2, err := svc.Create(ctx, issue.CreateIssueInput{
		ProjectKey:  "OMNIFY",
		Title:       "Suite de pruebas adversariales",
		Description: "Emulación de fallos bizantinos de red",
		Priority:    "high",
		StateName:   "Backlog",
	})
	if err != nil {
		t.Fatalf("Create offline task2 falló: %v", err)
	}

	task3, err := svc.Create(ctx, issue.CreateIssueInput{
		ProjectKey:  "OPS",
		Title:       "Mitigación de latencias en N8N",
		Description: "Optimizar pool de workers",
		Priority:    "medium",
		StateName:   "Backlog",
	})
	if err != nil {
		t.Fatalf("Create offline task3 falló: %v", err)
	}

	// Validar que se crearon con claves provisionales LOCAL
	if !strings.Contains(task1.Key, "-LOCAL-") || !strings.HasPrefix(task1.ID, "local-") {
		t.Errorf("task1 no tiene formato provisional LOCAL: ID=%s, Key=%s", task1.ID, task1.Key)
	}
	if !strings.Contains(task2.Key, "-LOCAL-") || !strings.HasPrefix(task2.ID, "local-") {
		t.Errorf("task2 no tiene formato provisional LOCAL: ID=%s, Key=%s", task2.ID, task2.Key)
	}
	if !strings.Contains(task3.Key, "-LOCAL-") || !strings.HasPrefix(task3.ID, "local-") {
		t.Errorf("task3 no tiene formato provisional LOCAL: ID=%s, Key=%s", task3.ID, task3.Key)
	}

	// 2. Asociar comentarios y worklogs mientras seguimos offline
	commentPayload1 := `{"comment_html":"<p>Bitácora de inicio en task1</p>"}`
	if err := store.EnqueueOutbox(ctx, "client-c1", task1.Key, commentPayload1, "teamprogra"); err != nil {
		t.Fatalf("error encolando comentario 1: %v", err)
	}

	commentPayload2 := `{"comment_html":"<p>Progreso adversarial verificado en task2</p>"}`
	if err := store.EnqueueOutbox(ctx, "client-c2", task2.Key, commentPayload2, "teamprogra"); err != nil {
		t.Fatalf("error encolando comentario 2: %v", err)
	}

	// Registrar entrada en el ledger de horas para task1 (con la clave LOCAL)
	now := time.Now()
	entry := &ledger.WorklogEntry{
		IssueID:  task1.Key,
		Interval: ledger.Interval{Start: now.Add(-30 * time.Minute), End: now},
		Bucket:   ledger.BucketDeepWork,
		Topic:    "Desarrollo local offline",
	}
	if err := store.SaveWorklog(ctx, entry); err != nil {
		t.Fatalf("error guardando worklog offline: %v", err)
	}

	// Verificar estado de la outbox offline: 3 creaciones + 2 comentarios = 5 registros pendientes
	pending, err := store.ListOutbox(ctx, false)
	if err != nil {
		t.Fatalf("error listando outbox: %v", err)
	}
	if len(pending) != 5 {
		t.Fatalf("se esperaban 5 registros pendientes en outbox, se encontraron %d", len(pending))
	}

	// 3. Intentar FlushOutbox mientras seguimos OFFLINE: debe fallar con gracia y no destruir la cola
	reportOffline, err := svc.FlushOutbox(ctx, 50)
	if err != nil {
		t.Fatalf("FlushOutbox no debió retornar error fatal: %v", err)
	}
	if reportOffline.SyncedIssues > 0 || reportOffline.SentComments > 0 {
		t.Errorf("no debió sincronizarse nada en offline: %+v", reportOffline)
	}
	if reportOffline.FailedComments != 5 {
		t.Errorf("se esperaban 5 fallos en reporte offline, se obtuvieron %d", reportOffline.FailedComments)
	}

	// Validar que los registros siguen en la outbox marcados como failed o pending para reintento
	pendingAfterFail, _ := store.ListOutbox(ctx, false)
	if len(pendingAfterFail) != 5 {
		t.Fatalf("la outbox no debe perder eventos tras fallos upstream: esperados 5, obtenidos %d", len(pendingAfterFail))
	}

	// FASE 2: RESTAURACIÓN DE CONECTIVIDAD UPSTREAM (ONLINE)
	mockClient.SetOnline(true)

	// 4. Ejecutar FlushOutbox para reconciliar todo
	reportOnline, err := svc.FlushOutbox(ctx, 50)
	if err != nil {
		t.Fatalf("FlushOutbox falló tras reconectar: %v", err)
	}

	if reportOnline.SyncedIssues != 3 {
		t.Errorf("se esperaban 3 tareas reconciliadas, se obtuvieron: %d (errores: %v)", reportOnline.SyncedIssues, reportOnline.Errors)
	}
	if reportOnline.SentComments != 2 {
		t.Errorf("se esperaban 2 comentarios entregados, se obtuvieron: %d (errores: %v)", reportOnline.SentComments, reportOnline.Errors)
	}
	if reportOnline.FailedComments != 0 {
		t.Errorf("no debieron quedar comentarios o creaciones fallidas: %d", reportOnline.FailedComments)
	}

	// 5. VALIDACIÓN RIGUROSA DE INTEGRIDAD TRAS MIGRACIÓN
	// a) Los tickets locales antiguos NO deben existir en issues_cache
	if oldItem, _ := store.GetIssue(ctx, task1.Key); oldItem != nil {
		t.Errorf("ticket local %s sigue existiendo en SQLite tras migración", task1.Key)
	}
	if oldItem, _ := store.GetIssue(ctx, task2.Key); oldItem != nil {
		t.Errorf("ticket local %s sigue existiendo en SQLite tras migración", task2.Key)
	}
	if oldItem, _ := store.GetIssue(ctx, task3.Key); oldItem != nil {
		t.Errorf("ticket local %s sigue existiendo en SQLite tras migración", task3.Key)
	}

	// b) Las tareas remotas creadas deben estar en SQLite con sus claves canónicas
	canonicalTask1Key := "OMNIFY-101"
	canonicalTask1, err := store.GetIssue(ctx, canonicalTask1Key)
	if err != nil || canonicalTask1 == nil {
		t.Fatalf("tarea canónica %s no encontrada en SQLite: %v", canonicalTask1Key, err)
	}
	if canonicalTask1.Title != "Arquitectura hexagonal resiliente" {
		t.Errorf("título corrompido tras migración: %s", canonicalTask1.Title)
	}
	if canonicalTask1.Priority != "urgent" {
		t.Errorf("prioridad no preservada tras migración: %s", canonicalTask1.Priority)
	}

	// c) Validar que el worklog fue migrado a la clave canónica
	timeReport, err := store.GetTimeReport(ctx, now.Add(-24*time.Hour).Format("2006-01-02"), now.Add(24*time.Hour).Format("2006-01-02"), "main", "")
	if err != nil {
		t.Fatalf("error obteniendo reporte de tiempo: %v", err)
	}
	if timeReport == nil || len(timeReport.Items) == 0 {
		t.Fatalf("worklog no encontrado tras reconciliación")
	}
	worklogMigrado := false
	for _, w := range timeReport.Items {
		if w.TicketKey == canonicalTask1Key {
			worklogMigrado = true
			break
		}
	}
	if !worklogMigrado {
		t.Errorf("el worklog no fue migrado a la clave canónica %s: %+v", canonicalTask1Key, timeReport.Items)
	}

	// d) Validar que los comentarios fueron entregados al mock con los UUIDs remotos oficiales
	mockClient.mu.RLock()
	defer mockClient.mu.RUnlock()
	commentsTask1 := mockClient.sentComments["iss-plane-uuid-101"]
	if len(commentsTask1) != 1 || commentsTask1[0] != commentPayload1 {
		t.Errorf("comentario no entregado al UUID remoto oficial iss-plane-uuid-101: %v", commentsTask1)
	}
	commentsTask2 := mockClient.sentComments["iss-plane-uuid-102"]
	if len(commentsTask2) != 1 || commentsTask2[0] != commentPayload2 {
		t.Errorf("comentario no entregado al UUID remoto oficial iss-plane-uuid-102: %v", commentsTask2)
	}

	// e) Validar que la outbox está 100% limpia (0 eventos pendientes)
	pendingFinal, _ := store.ListOutbox(ctx, false)
	if len(pendingFinal) != 0 {
		t.Errorf("la outbox debe estar vacía tras reconciliación total, registros pendientes: %d", len(pendingFinal))
	}
}

// TestAdversarial_Outbox_TransientErrorsAndReattempts prueba que un fallo transitorio
// de red no rompa el outbox y que tras varios reintentos con backoff se consolide.
func TestAdversarial_Outbox_TransientErrorsAndReattempts(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error abriendo SQLite: %v", err)
	}
	defer store.Close()

	_ = store.SaveProject(ctx, issue.ProjectRecord{Identifier: "OMNIFY", ProjectID: "proj-omnify", Name: "Omnify"})

	mockClient := newMockAdversarialClient(true)
	// Configurar para que falle exactamente 2 veces y tenga éxito en la 3ra
	mockClient.SetFailAttempts(2)

	svc := app.NewTrackerService(store, mockClient)

	// Crear una tarea local
	localItem := &issue.WorkItem{
		ID:         "local-test-backoff",
		Key:        "OMNIFY-LOCAL-9999",
		ProjectID:  "proj-omnify",
		ProjectKey: "OMNIFY",
		Title:      "Tarea con reintentos adversariales",
	}
	_ = store.SaveIssue(ctx, localItem)

	payloadBytes, _ := json.Marshal(issue.CreateIssueInput{
		ProjectKey: "OMNIFY",
		Title:      "Tarea con reintentos adversariales",
	})
	_ = store.EnqueueOutbox(ctx, "client-reintento", localItem.Key, string(payloadBytes), "")

	// Intento 1: debe fallar
	rep1, _ := svc.FlushOutbox(ctx, 10)
	if rep1.FailedComments != 1 || rep1.SyncedIssues != 0 {
		t.Errorf("intento 1 debió fallar: %+v", rep1)
	}

	// Intento 2: debe fallar
	rep2, _ := svc.FlushOutbox(ctx, 10)
	if rep2.FailedComments != 1 || rep2.SyncedIssues != 0 {
		t.Errorf("intento 2 debió fallar: %+v", rep2)
	}

	// Validar que se registraron attempts = 2
	outboxItems, _ := store.ListOutbox(ctx, true)
	if len(outboxItems) != 1 || outboxItems[0].Attempts != 2 {
		t.Errorf("contador de intentos no incrementado: %+v", outboxItems)
	}

	// Intento 3: debe tener éxito
	rep3, err := svc.FlushOutbox(ctx, 10)
	if err != nil || rep3.SyncedIssues != 1 {
		t.Fatalf("intento 3 debió reconciliar: rep=%+v, err=%v", rep3, err)
	}

	outboxItemsFinal, _ := store.ListOutbox(ctx, true)
	if len(outboxItemsFinal) != 1 || outboxItemsFinal[0].State != "sent" {
		t.Errorf("registro no marcado como sent en intento 3: %+v", outboxItemsFinal)
	}
}

// TestAdversarial_TrackerService_Concurrency_Stress valida la resiliencia y ausencia
// de data races bajo alta concurrencia de lecturas, escrituras y sincronizaciones de outbox.
func TestAdversarial_TrackerService_Concurrency_Stress(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "allostasis_app_stress.db")

	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("error creando store en archivo: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_ = store.SaveProject(ctx, issue.ProjectRecord{Identifier: "OMNIFY", ProjectID: "proj-stress", Name: "Omnify Stress"})
	_ = store.SaveState(ctx, issue.StateRecord{ID: "st-stress-1", ProjectID: "proj-stress", Name: "Backlog", GroupName: "backlog"})

	mockClient := newMockAdversarialClient(true)
	svc := app.NewTrackerService(store, mockClient)

	const numWorkers = 15
	const opsPerWorker = 20

	var wg sync.WaitGroup
	var errorCount int64

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				op := (workerID + j) % 5
				switch op {
				case 0:
					// Create
					_, err := svc.Create(ctx, issue.CreateIssueInput{
						ProjectKey: "OMNIFY",
						Title:      fmt.Sprintf("Concurrent task %d-%d", workerID, j),
						Priority:   "high",
					})
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
					}
				case 1:
					// Search
					_, err := svc.Search(ctx, fmt.Sprintf("task %d", workerID), "OMNIFY", 5)
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
					}
				case 2:
					// List
					_, err := svc.List(ctx, issue.IssueFilter{ProjectKey: "OMNIFY", Limit: 10})
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
					}
				case 3:
					// FlushOutbox
					_, err := svc.FlushOutbox(ctx, 5)
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
					}
				case 4:
					// ListOutbox
					_, err := svc.ListOutbox(ctx, false)
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
					}
				}
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Prueba de estrés concurrente completada. Errores totales capturados: %d", atomic.LoadInt64(&errorCount))
	if errorCount > 0 {
		t.Errorf("Se presentaron %d errores durante la ejecución concurrente bajo -race", errorCount)
	}
}

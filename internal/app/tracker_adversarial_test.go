package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
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
		{Identifier: "OMNIFY", ProjectID: "proj-uuid-omnify", Name: "Omnify Core", Workspace: "test-workspace"},
		{Identifier: "OPS", ProjectID: "proj-uuid-ops", Name: "Operations", Workspace: "test-workspace"},
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

func (m *mockAdversarialTrackerClient) FetchIssuesDelta(ctx context.Context, projectID, projectKey string, limit int, cutoffISO string, deep bool) ([]issue.IssueSnapshot, string, error) {
	snaps, err := m.FetchIssues(ctx, projectID, projectKey, limit)
	newest := ""
	if len(snaps) > 0 && snaps[0].Item.RemoteUpdatedAt != nil {
		newest = snaps[0].Item.RemoteUpdatedAt.Format(time.RFC3339)
	}
	return snaps, newest, err
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

func (m *mockAdversarialTrackerClient) FetchComments(ctx context.Context, projectID, issueID string) ([]issue.CommentRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.isOnline {
		return nil, fmt.Errorf("Plane CE offline")
	}
	return nil, nil
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
	if err := store.EnqueueOutbox(ctx, "client-c1", task1.Key, commentPayload1, "test-workspace"); err != nil {
		t.Fatalf("error encolando comentario 1: %v", err)
	}

	commentPayload2 := `{"comment_html":"<p>Progreso adversarial verificado en task2</p>"}`
	if err := store.EnqueueOutbox(ctx, "client-c2", task2.Key, commentPayload2, "test-workspace"); err != nil {
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

// ---------------------------------------------------------------------------
// SUITE ADVERSARIAL: CONCURRENCIA, RESILIENCIA Y CAOS EN SYNC
// ---------------------------------------------------------------------------

// mockChaosTrackerClient modela un backend de Plane CE con inyección controlada de caos:
// latencias asíncronas con jitter, fallos HTTP 500/503 simulados, caídas de socket y rastreo
// de llamadas y goroutines en vuelo para verificar ausencia de leaks.
type mockChaosTrackerClient struct {
	*mockAdversarialTrackerClient
	mu            sync.RWMutex
	projects      []issue.ProjectRecord
	states        map[string][]issue.StateRecord
	labels        map[string][]issue.Label
	issues        map[string][]issue.IssueSnapshot
	failProjects  map[string]error // projectKey o projectID -> error inducido
	failFetchProj error
	minLatency    time.Duration
	maxLatency    time.Duration
	activeCalls   int64
	totalCalls    int64
	jitterCounter int64
}

func newMockChaosTrackerClient() *mockChaosTrackerClient {
	return &mockChaosTrackerClient{
		mockAdversarialTrackerClient: newMockAdversarialClient(true),
		states:                       make(map[string][]issue.StateRecord),
		labels:                       make(map[string][]issue.Label),
		issues:                       make(map[string][]issue.IssueSnapshot),
		failProjects:                 make(map[string]error),
	}
}

func (m *mockChaosTrackerClient) SetLatency(min, max time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.minLatency = min
	m.maxLatency = max
}

func (m *mockChaosTrackerClient) SetProjectFailure(keyOrID string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failProjects[keyOrID] = err
}

func (m *mockChaosTrackerClient) delay(ctx context.Context) error {
	m.mu.RLock()
	minL := m.minLatency
	maxL := m.maxLatency
	m.mu.RUnlock()

	if minL <= 0 && maxL <= 0 {
		return ctx.Err()
	}

	diff := maxL - minL
	d := minL
	if diff > 0 {
		count := atomic.AddInt64(&m.jitterCounter, 1)
		d += time.Duration(count % int64(diff))
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (m *mockChaosTrackerClient) FetchProjects(ctx context.Context) ([]issue.ProjectRecord, error) {
	atomic.AddInt64(&m.activeCalls, 1)
	defer atomic.AddInt64(&m.activeCalls, -1)
	atomic.AddInt64(&m.totalCalls, 1)

	if err := m.delay(ctx); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.failFetchProj != nil {
		return nil, m.failFetchProj
	}
	return m.projects, nil
}

func (m *mockChaosTrackerClient) FetchStates(ctx context.Context, projectID string) ([]issue.StateRecord, error) {
	atomic.AddInt64(&m.activeCalls, 1)
	defer atomic.AddInt64(&m.activeCalls, -1)
	atomic.AddInt64(&m.totalCalls, 1)

	if err := m.delay(ctx); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if err, fail := m.failProjects[projectID]; fail && err != nil {
		return nil, err
	}
	return m.states[projectID], nil
}

func (m *mockChaosTrackerClient) FetchLabels(ctx context.Context, projectID string) ([]issue.Label, error) {
	atomic.AddInt64(&m.activeCalls, 1)
	defer atomic.AddInt64(&m.activeCalls, -1)
	atomic.AddInt64(&m.totalCalls, 1)

	if err := m.delay(ctx); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if err, fail := m.failProjects[projectID]; fail && err != nil {
		return nil, err
	}
	return m.labels[projectID], nil
}

func (m *mockChaosTrackerClient) FetchIssues(ctx context.Context, projectID, projectKey string, limit int) ([]issue.IssueSnapshot, error) {
	atomic.AddInt64(&m.activeCalls, 1)
	defer atomic.AddInt64(&m.activeCalls, -1)
	atomic.AddInt64(&m.totalCalls, 1)

	if err := m.delay(ctx); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if err, fail := m.failProjects[projectKey]; fail && err != nil {
		return nil, err
	}
	if err, fail := m.failProjects[projectID]; fail && err != nil {
		return nil, err
	}

	if snk, ok := m.issues[projectKey]; ok {
		return snk, nil
	}
	return m.issues[projectID], nil
}

func (m *mockChaosTrackerClient) FetchIssuesDelta(ctx context.Context, projectID, projectKey string, limit int, cutoffISO string, deep bool) ([]issue.IssueSnapshot, string, error) {
	snaps, err := m.FetchIssues(ctx, projectID, projectKey, limit)
	newest := ""
	if len(snaps) > 0 && snaps[0].Item.RemoteUpdatedAt != nil {
		newest = snaps[0].Item.RemoteUpdatedAt.Format(time.RFC3339)
	}
	return snaps, newest, err
}

func (m *mockChaosTrackerClient) AddProjectWithData(identifier, name string, issueCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	projID := "proj-uuid-" + strings.ToLower(identifier)
	m.projects = append(m.projects, issue.ProjectRecord{
		Identifier: identifier,
		ProjectID:  projID,
		Name:       name,
		Workspace:  "test-workspace",
	})

	stateBacklogID := fmt.Sprintf("st-backlog-%s", strings.ToLower(identifier))
	stateStartedID := fmt.Sprintf("st-started-%s", strings.ToLower(identifier))
	m.states[projID] = []issue.StateRecord{
		{ID: stateBacklogID, ProjectID: projID, Name: "Backlog", Slug: "backlog", GroupName: "backlog"},
		{ID: stateStartedID, ProjectID: projID, Name: "In Progress", Slug: "in_progress", GroupName: "started"},
	}

	m.labels[projID] = []issue.Label{
		{ID: fmt.Sprintf("lbl-%s-1", strings.ToLower(identifier)), ProjectID: projID, Name: "backend"},
		{ID: fmt.Sprintf("lbl-%s-2", strings.ToLower(identifier)), ProjectID: projID, Name: "infra"},
	}

	var snapshots []issue.IssueSnapshot
	for i := 1; i <= issueCount; i++ {
		issueKey := fmt.Sprintf("%s-%d", identifier, i)
		issueID := fmt.Sprintf("iss-%s-uuid-%d", strings.ToLower(identifier), i)
		targetStateID := stateStartedID
		if i%2 == 0 {
			targetStateID = stateBacklogID
		}
		rawJSON := fmt.Sprintf(`{"id":%q,"state":%q,"name":%q}`, issueID, targetStateID, fmt.Sprintf("Issue %s", issueKey))

		snapshots = append(snapshots, issue.IssueSnapshot{
			Item: issue.WorkItem{
				ID:         issueID,
				Key:        issueKey,
				ProjectID:  projID,
				ProjectKey: identifier,
				SequenceID: i,
				Title:      fmt.Sprintf("Issue %s", issueKey),
				Priority:   "high",
				StateName:  "", // Vacío a propósito: valida mapeo concurrente desde SyncStates
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			},
			RawJSON: rawJSON,
		})
	}
	m.issues[projID] = snapshots
	m.issues[identifier] = snapshots
}

// TestAdversarial_Sync_MassiveConcurrencyWithJitter somete el worker pool de Sync (maxWorkers=5)
// a una carga masiva de 40 proyectos simultáneos (120 issues en total) con latencias asíncronas
// y jitter aleatorio (1-10ms) en cada upstream call.
// Valida:
// 1. Que los 40 proyectos y 120 issues se sincronicen por completo sin pérdida de datos.
// 2. Que el mapeo concurrente de estados desde SyncStates hacia FetchIssues sea atómico y preciso.
// 3. Que el SQLite en disco con modo WAL y doble pool maneje las escrituras concurrentes sin sqlite_busy.
// 4. Que no existan llamadas colgadas o fugas de llamadas en el cliente mock.
func TestAdversarial_Sync_MassiveConcurrencyWithJitter(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "alo_massive_sync.db")

	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("error abriendo SQLite WAL en disco: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	mockClient := newMockChaosTrackerClient()
	mockClient.SetLatency(1*time.Millisecond, 8*time.Millisecond)

	const projectCount = 40
	const issuesPerProject = 3
	const expectedTotalIssues = projectCount * issuesPerProject

	for i := 1; i <= projectCount; i++ {
		key := fmt.Sprintf("PROJ%02d", i)
		name := fmt.Sprintf("Proyecto Concurrente %02d", i)
		mockClient.AddProjectWithData(key, name, issuesPerProject)
	}

	svc := app.NewTrackerService(store, mockClient)

	start := time.Now()
	totalSynced, err := svc.Sync(ctx, "", true)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Sync masivo falló inesperadamente: %v", err)
	}

	if totalSynced != expectedTotalIssues {
		t.Fatalf("totalSynced incorrecto: esperados %d, obtenidos %d", expectedTotalIssues, totalSynced)
	}

	t.Logf("Sync masivo de %d proyectos (%d tareas) completado en %v", projectCount, totalSynced, elapsed)

	// Verificación de integridad en la base de datos SQLite
	projectsInDB, err := store.ListProjects(ctx)
	if err != nil {
		t.Fatalf("error listando proyectos en DB: %v", err)
	}
	if len(projectsInDB) != projectCount {
		t.Fatalf("se esperaban %d proyectos en DB, encontrados %d", projectCount, len(projectsInDB))
	}

	// Verificar resolución de estados para cada tarea
	for i := 1; i <= projectCount; i++ {
		keyPrefix := fmt.Sprintf("PROJ%02d", i)
		for j := 1; j <= issuesPerProject; j++ {
			issueKey := fmt.Sprintf("%s-%d", keyPrefix, j)
			item, err := store.GetIssue(ctx, issueKey)
			if err != nil || item == nil {
				t.Fatalf("tarea %s no persistida en DB: %v", issueKey, err)
			}
			if item.StateName == "" || item.StateGroup == "" {
				t.Errorf("tarea %s no resolvió su estado concurrentemente: StateName=%q, StateGroup=%q",
					issueKey, item.StateName, item.StateGroup)
			}
			if j%2 == 0 {
				if item.StateName != "Backlog" || item.StateGroup != "backlog" {
					t.Errorf("tarea %s esperaba Backlog/backlog, obtuvo %s/%s", issueKey, item.StateName, item.StateGroup)
				}
			} else {
				if item.StateName != "In Progress" || item.StateGroup != "started" {
					t.Errorf("tarea %s esperaba In Progress/started, obtuvo %s/%s", issueKey, item.StateName, item.StateGroup)
				}
			}
		}
	}

	// Verificar que no queden llamadas en vuelo en el cliente
	if active := atomic.LoadInt64(&mockClient.activeCalls); active != 0 {
		t.Errorf("se detectaron %d llamadas en vuelo residuales en el cliente mock", active)
	}
}

// TestAdversarial_Sync_UpstreamChaosAndPartialFailures somete a Sync a fallos caóticos
// upstream: 10 de 30 proyectos fallan intencionalmente con HTTP 500, HTTP 503, caídas de
// conexión y timeouts, mientras los otros 20 proyectos responden con normalidad.
// Valida:
// 1. Que Sync procese con gracia los proyectos sanos sin abortar el lote completo por fallos individuales.
// 2. Que el contador totalSynced refleje de forma determinista la suma exacta de las tareas de proyectos sanos (60).
// 3. Que las tareas de proyectos fallidos no queden corruptas o incompletas en la base de datos.
// 4. Que no existan deadlocks en el canal de jobs ni en el canal de results.
func TestAdversarial_Sync_UpstreamChaosAndPartialFailures(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "alo_chaos_sync.db")

	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("error abriendo SQLite: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	mockClient := newMockChaosTrackerClient()
	mockClient.SetLatency(1*time.Millisecond, 6*time.Millisecond)

	const healthyProjects = 20
	const faultyProjects = 10
	const issuesPerProject = 3
	const expectedHealthyIssues = healthyProjects * issuesPerProject

	// Inyectar 20 proyectos sanos
	for i := 1; i <= healthyProjects; i++ {
		key := fmt.Sprintf("SAN%02d", i)
		mockClient.AddProjectWithData(key, fmt.Sprintf("Proyecto Sano %02d", i), issuesPerProject)
	}

	// Inyectar 10 proyectos con diferentes modos de fallo caóticos
	for i := 1; i <= faultyProjects; i++ {
		key := fmt.Sprintf("CHAOS%02d", i)
		mockClient.AddProjectWithData(key, fmt.Sprintf("Proyecto Caos %02d", i), issuesPerProject)

		var injectedErr error
		switch i % 4 {
		case 0:
			injectedErr = fmt.Errorf("HTTP 500 Internal Server Error: plane database transaction deadlock")
		case 1:
			injectedErr = fmt.Errorf("HTTP 503 Service Unavailable: rate limit quota exhausted")
		case 2:
			injectedErr = fmt.Errorf("read tcp 10.0.0.1:8000: connection reset by peer")
		case 3:
			injectedErr = fmt.Errorf("i/o timeout: plane backend gateway unresponsive")
		}
		mockClient.SetProjectFailure(key, injectedErr)
	}

	svc := app.NewTrackerService(store, mockClient)

	totalSynced, err := svc.Sync(ctx, "", true)
	if err != nil {
		t.Fatalf("Sync con fallos parciales falló fatalmente: %v", err)
	}

	if totalSynced != expectedHealthyIssues {
		t.Fatalf("totalSynced inconsistente: esperados %d (sólo sanos), obtenidos %d", expectedHealthyIssues, totalSynced)
	}

	t.Logf("Sync con caos upstream completado: %d tareas sincronizadas de 20 proyectos sanos (10 proyectos con fallo amortiguados)", totalSynced)

	// Validar que las tareas sanas existen en la base de datos
	for i := 1; i <= healthyProjects; i++ {
		keyPrefix := fmt.Sprintf("SAN%02d", i)
		for j := 1; j <= issuesPerProject; j++ {
			issueKey := fmt.Sprintf("%s-%d", keyPrefix, j)
			item, err := store.GetIssue(ctx, issueKey)
			if err != nil || item == nil {
				t.Fatalf("tarea sana %s debió ser guardada: %v", issueKey, err)
			}
		}
	}

	// Validar que ninguna tarea de los proyectos con fallo exista en SQLite
	for i := 1; i <= faultyProjects; i++ {
		keyPrefix := fmt.Sprintf("CHAOS%02d", i)
		for j := 1; j <= issuesPerProject; j++ {
			issueKey := fmt.Sprintf("%s-%d", keyPrefix, j)
			item, _ := store.GetIssue(ctx, issueKey)
			if item != nil {
				t.Errorf("tarea de proyecto fallido %s no debió guardarse", issueKey)
			}
		}
	}

	if active := atomic.LoadInt64(&mockClient.activeCalls); active != 0 {
		t.Errorf("llamadas activas residuales tras caos upstream: %d", active)
	}
}

// TestAdversarial_Sync_AbruptContextCancellation valida el comportamiento del sistema
// ante cancelaciones abruptas de contexto (context.WithCancel y context.WithTimeout) tanto
// en el pool paralelo de Sync como en las sub-goroutines de SyncIssues.
// Valida:
// 1. Que no existan fugas de goroutines (goroutine leaks) y los workers aborten limpiamente.
// 2. Que se retorne context.Canceled o context.DeadlineExceeded sin causar panics.
// 3. Que el canal jobs se drene o se cierre adecuadamente sin deadlocks.
func TestAdversarial_Sync_AbruptContextCancellation(t *testing.T) {
	t.Run("MidFlight_ContextCancel", func(t *testing.T) {
		tempDir := t.TempDir()
		store, err := sqlite.NewStore(filepath.Join(tempDir, "alo_cancel.db"))
		if err != nil {
			t.Fatalf("error creando SQLite: %v", err)
		}
		defer store.Close()

		mockClient := newMockChaosTrackerClient()
		// Latencia suficiente para asegurar que la cancelación ocurre durante la ejecución de los workers
		mockClient.SetLatency(15*time.Millisecond, 35*time.Millisecond)

		const projectCount = 30
		for i := 1; i <= projectCount; i++ {
			mockClient.AddProjectWithData(fmt.Sprintf("CAN%02d", i), fmt.Sprintf("Cancel Project %02d", i), 3)
		}

		svc := app.NewTrackerService(store, mockClient)

		initialGoroutines := runtime.NumGoroutine()
		ctx, cancel := context.WithCancel(context.Background())

		type syncOut struct {
			count int
			err   error
		}
		doneCh := make(chan syncOut, 1)

		go func() {
			count, syncErr := svc.Sync(ctx, "", true)
			doneCh <- syncOut{count: count, err: syncErr}
		}()

		// Esperar que los workers comiencen a procesar trabajos y luego cancelar inmediatamente
		time.Sleep(20 * time.Millisecond)
		cancel()

		select {
		case out := <-doneCh:
			if out.err == nil {
				t.Fatalf("se esperaba error de contexto cancelado, pero retornó nil")
			}
			if !errors.Is(out.err, context.Canceled) && !strings.Contains(out.err.Error(), "context canceled") {
				t.Errorf("error esperado context.Canceled, obtenido: %v", out.err)
			}
			t.Logf("Cancelación mid-flight recibida limpiamente: err=%v, count=%d", out.err, out.count)
		case <-time.After(3 * time.Second):
			t.Fatalf("DEADLOCK detectado: Sync no retornó tras la cancelación de contexto")
		}

		// Esperar hasta 200ms para asegurar que las goroutines en vuelo del mock retornen
		deadline := time.Now().Add(250 * time.Millisecond)
		for time.Now().Before(deadline) {
			if atomic.LoadInt64(&mockClient.activeCalls) == 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		if active := atomic.LoadInt64(&mockClient.activeCalls); active != 0 {
			t.Errorf("fuga detectada: quedan %d llamadas activas en vuelo tras cancelación", active)
		}

		// Verificar que el número de goroutines del runtime retorne al nivel basal (sin leaks)
		leaked := true
		for i := 0; i < 20; i++ {
			if runtime.NumGoroutine() <= initialGoroutines+2 {
				leaked = false
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if leaked {
			t.Errorf("fuga de goroutines detectada: iniciales=%d, actuales=%d", initialGoroutines, runtime.NumGoroutine())
		}
	})

	t.Run("Timeout_DeadlineExceeded", func(t *testing.T) {
		tempDir := t.TempDir()
		store, err := sqlite.NewStore(filepath.Join(tempDir, "alo_timeout.db"))
		if err != nil {
			t.Fatalf("error creando SQLite: %v", err)
		}
		defer store.Close()

		mockClient := newMockChaosTrackerClient()
		// Cada llamada tarda al menos 30ms, con timeout global de 10ms
		mockClient.SetLatency(30*time.Millisecond, 50*time.Millisecond)

		for i := 1; i <= 20; i++ {
			mockClient.AddProjectWithData(fmt.Sprintf("TIM%02d", i), fmt.Sprintf("Timeout Proj %02d", i), 2)
		}

		svc := app.NewTrackerService(store, mockClient)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		doneCh := make(chan error, 1)
		go func() {
			_, err := svc.Sync(ctx, "", true)
			doneCh <- err
		}()

		select {
		case err := <-doneCh:
			if err == nil {
				t.Fatalf("se esperaba error de deadline exceeded, pero retornó nil")
			}
			if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context deadline exceeded") {
				t.Errorf("error esperado context.DeadlineExceeded, obtenido: %v", err)
			}
			t.Logf("Timeout verificado exitosamente: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatalf("DEADLOCK detectado: Sync no respetó context.DeadlineExceeded")
		}

		// Verificar drenaje de llamadas
		time.Sleep(100 * time.Millisecond)
		if active := atomic.LoadInt64(&mockClient.activeCalls); active != 0 {
			t.Errorf("fuga de llamadas tras timeout: %d activas", active)
		}
	})

	t.Run("SingleProject_SyncIssues_Timeout", func(t *testing.T) {
		tempDir := t.TempDir()
		store, err := sqlite.NewStore(filepath.Join(tempDir, "alo_single_timeout.db"))
		if err != nil {
			t.Fatalf("error creando SQLite: %v", err)
		}
		defer store.Close()

		mockClient := newMockChaosTrackerClient()
		mockClient.SetLatency(40*time.Millisecond, 60*time.Millisecond)
		mockClient.AddProjectWithData("SINGLE", "Single Project", 5)

		svc := app.NewTrackerService(store, mockClient)

		// Ejecutar con un timeout de 10ms
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		count, err := svc.SyncIssues(ctx, "SINGLE", 100)
		if err == nil {
			t.Fatalf("se esperaba error por context.DeadlineExceeded, obtenido nil")
		}
		if count != 0 {
			t.Errorf("se esperaba count=0 tras timeout, obtenido %d", count)
		}
		if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline exceeded") {
			t.Errorf("se esperaba error de timeout, recibido: %v", err)
		}

		// Validar que las 3 goroutines internas (SyncStates, SyncLabels, FetchIssues) se detuvieron
		time.Sleep(100 * time.Millisecond)
		if active := atomic.LoadInt64(&mockClient.activeCalls); active != 0 {
			t.Errorf("fuga de sub-goroutines en SyncIssues: %d llamadas activas", active)
		}
	})
}

// TestAdversarial_SyncIssues_SingleProjectConcurrency valida el comportamiento y la
// sincronización interna de goroutines concurrentes (SyncStates, SyncLabels y FetchIssues)
// dentro de SyncIssues.
// Valida:
// 1. Descarga concurrente sin condiciones de carrera de estados, etiquetas y tareas.
// 2. Resolución dinámica de ProjectID cuando el proyecto no está cacheador previamente.
// 3. Mapeo y enriquecimiento correcto de StateName y StateGroup a partir de stateMap.
// 4. Inserción correcta de Snapshots JSON crudos en issues_snapshots.
func TestAdversarial_SyncIssues_SingleProjectConcurrency(t *testing.T) {
	tempDir := t.TempDir()
	store, err := sqlite.NewStore(filepath.Join(tempDir, "alo_single_proj.db"))
	if err != nil {
		t.Fatalf("error abriendo SQLite: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	mockClient := newMockChaosTrackerClient()
	mockClient.SetLatency(2*time.Millisecond, 8*time.Millisecond)

	const issueCount = 10
	mockClient.AddProjectWithData("OMNIFY", "Omnify Core", issueCount)

	svc := app.NewTrackerService(store, mockClient)

	// Ejecutar SyncIssues sin precachear proyecto en SQLite
	count, err := svc.SyncIssues(ctx, "OMNIFY", 50)
	if err != nil {
		t.Fatalf("SyncIssues falló: %v", err)
	}

	if count != issueCount {
		t.Fatalf("se esperaban %d issues sincronizados, obtenidos %d", issueCount, count)
	}

	// Verificar estados y snapshots en SQLite
	for i := 1; i <= issueCount; i++ {
		key := fmt.Sprintf("OMNIFY-%d", i)
		item, err := store.GetIssue(ctx, key)
		if err != nil || item == nil {
			t.Fatalf("issue %s no encontrado en SQLite: %v", key, err)
		}
		if item.StateName == "" || item.StateGroup == "" {
			t.Errorf("issue %s tiene campos de estado vacíos tras SyncIssues: %+v", key, item)
		}
	}

	// Verificar que las etiquetas fueron persistidas
	labels, err := store.ListLabels(ctx, "proj-uuid-omnify")
	if err != nil || len(labels) == 0 {
		t.Errorf("las etiquetas del proyecto OMNIFY no se guardaron concurrentemente: err=%v, count=%d", err, len(labels))
	}
}

// TestAdversarial_Sync_RaceAndContention somete el TrackerService y la base de datos
// SQLite a lectura, escritura, sincronización masiva y búsquedas simultáneas de múltiples
// goroutines concurrentes bajo el detector de carreras de Go (-race).
func TestAdversarial_Sync_RaceAndContention(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "alo_race_contention.db")

	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("error abriendo SQLite en disco: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	mockClient := newMockChaosTrackerClient()
	mockClient.SetLatency(1*time.Millisecond, 4*time.Millisecond)

	for i := 1; i <= 8; i++ {
		mockClient.AddProjectWithData(fmt.Sprintf("RACE%d", i), fmt.Sprintf("Race Project %d", i), 4)
	}

	svc := app.NewTrackerService(store, mockClient)

	// Inicializar proyecto base
	_, _ = svc.Sync(ctx, "", true)

	const numWorkers = 8
	const opsPerWorker = 15

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
					// Sync all
					_, err := svc.Sync(ctx, "", true)
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
					}
				case 1:
					// Sync individual project
					projKey := fmt.Sprintf("RACE%d", (workerID%8)+1)
					_, err := svc.SyncIssues(ctx, projKey, 100)
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
					}
				case 2:
					// Search
					_, err := svc.Search(ctx, "Issue", "", 5)
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
					}
				case 3:
					// List
					_, err := svc.List(ctx, issue.IssueFilter{Limit: 10})
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
					}
				case 4:
					// Create local issue
					_, err := svc.Create(ctx, issue.CreateIssueInput{
						ProjectKey: "RACE1",
						Title:      fmt.Sprintf("Concurrent task from race worker %d-%d", workerID, j),
						Priority:   "medium",
					})
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
					}
				}
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Prueba de contención y carreras completada. Errores totales capturados: %d", atomic.LoadInt64(&errorCount))
	if count := atomic.LoadInt64(&errorCount); count > 0 {
		t.Errorf("se detectaron %d errores durante la ejecución concurrente bajo -race", count)
	}
}

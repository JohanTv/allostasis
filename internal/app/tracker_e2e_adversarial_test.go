package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/adapters/tracker_plane"
	"github.com/allostasis/alo/internal/app"
	"github.com/allostasis/alo/internal/domain/clock"
	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/domain/ledger"
)

// mockChaosServer encapsula un servidor HTTP con inyección de caos determinista
// capaz de simular fallos HTTP (500, 502, 503), cortes de red abruptos (TCP Hijack/Close),
// respuestas ambiguas y paginación con parada temprana.
type mockChaosServer struct {
	mu               sync.RWMutex
	mode             string // "normal", "500", "502", "503", "disconnect", "ambiguous", "timeout"
	commentsReceived []string
	callCount        int64
	issues           []map[string]interface{}
}

func newMockChaosServer() *mockChaosServer {
	return &mockChaosServer{
		mode:             "normal",
		commentsReceived: make([]string, 0),
		issues:           make([]map[string]interface{}, 0),
	}
}

func (s *mockChaosServer) SetMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = mode
}

func (s *mockChaosServer) GetMode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mode
}

func (s *mockChaosServer) GetCommentsCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.commentsReceived)
}

func (s *mockChaosServer) SetIssues(issues []map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.issues = issues
}

func (s *mockChaosServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&s.callCount, 1)
	mode := s.GetMode()

	// Inyección de desconexión abrupta por TCP reset
	if mode == "disconnect" {
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			if conn != nil {
				_ = conn.Close()
				return
			}
		}
		http.Error(w, "Connection reset", http.StatusBadGateway)
		return
	}

	// Inyección de timeout
	if mode == "timeout" {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
		return
	}

	// Enrutamiento por path
	path := r.URL.Path

	// 1. Manejo de comentarios (/comments/)
	if strings.Contains(path, "/comments/") && r.Method == http.MethodPost {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		rawJSON, _ := json.Marshal(body)

		s.mu.Lock()
		s.commentsReceived = append(s.commentsReceived, string(rawJSON))
		cmtIdx := len(s.commentsReceived)
		s.mu.Unlock()

		// Fallos HTTP explícitos
		switch mode {
		case "500":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error": "Internal Server Error en Plane CE"}`))
			return
		case "502":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error": "Bad Gateway upstream"}`))
			return
		case "503":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error": "Service Unavailable Plane CE"}`))
			return
		case "ambiguous":
			// El servidor procesó el comentario pero se corta la conexión antes de que el cliente reciba el ACK
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, _ := hj.Hijack()
				if conn != nil {
					_ = conn.Close()
					return
				}
			}
			return
		}

		// Modo normal: respuesta HTTP 201 exitosa
		w.WriteHeader(http.StatusCreated)
		resp := map[string]interface{}{
			"id":         fmt.Sprintf("cmt-remote-%d", cmtIdx),
			"created_at": time.Now().Format(time.RFC3339),
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// 2. Manejo de Estados (/states/)
	if strings.Contains(path, "/states/") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": "st-backlog", "name": "Backlog", "group": "backlog"},
			{"id": "st-started", "name": "Started", "group": "started"},
			{"id": "st-completed", "name": "Completed", "group": "completed"},
		})
		return
	}

	// 3. Manejo de Etiquetas (/labels/)
	if strings.Contains(path, "/labels/") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": "lbl-backend", "name": "backend", "color": "#ff0000"},
		})
		return
	}

	// 4. Manejo de Proyectos (/projects/)
	if strings.HasSuffix(path, "/projects/") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": "proj-omnify", "identifier": "OMNIFY", "name": "Omnify Core"},
			{"id": "proj-ops", "identifier": "OPS", "name": "Operations Core"},
		})
		return
	}

	// 5. Manejo de Issues Paginados (/issues/)
	if strings.Contains(path, "/issues/") && r.Method == http.MethodGet {
		s.mu.RLock()
		allIssues := s.issues
		s.mu.RUnlock()

		cursor := r.URL.Query().Get("cursor")
		startIdx := 0
		if cursor == "page2" && len(allIssues) >= 2 {
			startIdx = 2
		} else if cursor == "page3" && len(allIssues) >= 4 {
			startIdx = 4
		}

		limit := 2
		endIdx := startIdx + limit
		if endIdx > len(allIssues) {
			endIdx = len(allIssues)
		}

		pageItems := allIssues[startIdx:endIdx]
		nextCursor := ""
		hasNext := false
		if endIdx < len(allIssues) {
			if startIdx == 0 {
				nextCursor = "page2"
				hasNext = true
			} else if startIdx == 2 {
				nextCursor = "page3"
				hasNext = true
			}
		}

		resp := map[string]interface{}{
			"next_cursor":       nextCursor,
			"next_page_results": hasNext,
			"results":           pageItems,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// Default fallback
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}

// TestE2EAdversarial_OutboxLifecycle_UnderChaos valida integralmente el ciclo de vida
// del Outbox bajo condiciones hostiles y fallos transitorios de red:
// 1. Encolamiento de comentarios y horas con metadatos PLANE_WORKLOG_METADATA.
// 2. Caídas transitorias de Plane CE (HTTP 500, 502, 503, timeouts y desconexiones de red).
// 3. Verificación de incremento de attempts, last_error y preservación sin pérdida de items.
// 4. Recuperación de red: transmisión exitosa y transición atómica a estado 'sent'.
// 5. Vinculación en tabla worklogs de plane_comment_id para trazabilidad.
// 6. Idempotencia ante drenajes redundantes y reintentos tras ACK ambiguo.
func TestE2EAdversarial_OutboxLifecycle_UnderChaos(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "outbox_chaos_e2e.db")

	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("error inicializando SQLite en disco con WAL: %v", err)
	}
	defer store.Close()

	// Inicializar proyecto y ticket canónico en SQLite
	_ = store.SaveProject(ctx, issue.ProjectRecord{
		Identifier: "OMNIFY",
		ProjectID:  "proj-omnify",
		Name:       "Omnify Core",
		Workspace:  "test-ws",
	})

	testTicket := &issue.WorkItem{
		ID:         "iss-omnify-101",
		Key:        "OMNIFY-101",
		ProjectID:  "proj-omnify",
		ProjectKey: "OMNIFY",
		SequenceID: 101,
		Title:      "Investigación de Resiliencia Alostática",
		StateName:  "Backlog",
		StateGroup: "backlog",
		State:      issue.StateBacklog,
		Priority:   "high",
		Assignee:   "usr-chaos",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := store.SaveIssue(ctx, testTicket); err != nil {
		t.Fatalf("error guardando ticket de prueba: %v", err)
	}

	// Inicializar mock server HTTP de Plane CE
	chaosSrv := newMockChaosServer()
	httpSrv := httptest.NewServer(chaosSrv)
	defer httpSrv.Close()

	planeClient := tracker_plane.NewClient(tracker_plane.Config{
		BaseURL:   httpSrv.URL,
		APIKey:    "test-chaos-api-key",
		Workspace: "test-ws",
		Timeout:   1 * time.Second,
	})

	trackerSvc := app.NewTrackerService(store, planeClient)
	frozenTime := time.Date(2026, 11, 16, 14, 0, 0, 0, time.UTC)
	clk := clock.NewFrozenClock(frozenTime)
	cockpitSvc := app.NewCockpitService(store, store, nil, clk)

	// =========================================================================
	// FASE 1: Encolamiento de comentarios y horas telemétricas (PLANE_WORKLOG_METADATA)
	// =========================================================================
	t.Log("FASE 1: Encolando trabajo telemétrico y comentarios...")

	// 1.1 Registrar sesión telemétrica con CockpitService.LogTime
	err = cockpitSvc.LogTime(ctx, "OMNIFY-101", 90, ledger.BucketDeepWork, "Diseño de tests adversariales y resiliencia")
	if err != nil {
		t.Fatalf("error al registrar tiempo con LogTime: %v", err)
	}

	// 1.2 Registrar comentario con horas telemétricas en TrackerService.AddComment (forzando offline o fallo)
	chaosSrv.SetMode("500") // Servidor falla inmediatamente para forzar persistencia en outbox
	cmtRec, err := trackerSvc.AddComment(ctx, "OMNIFY-101", "Revisión técnica de arquitectura hexagonal", 1.5, "10:00", "main")
	if err != nil {
		t.Fatalf("AddComment retornó error inesperado: %v", err)
	}
	if cmtRec == nil || !strings.Contains(cmtRec.CommentHTML, "PLANE_WORKLOG_METADATA") {
		t.Fatalf("el comentario no incluye el bloque estandarizado PLANE_WORKLOG_METADATA: %+v", cmtRec)
	}

	// 1.3 Verificar estado inicial de la outbox en SQLite
	outboxItems, err := store.ListOutbox(ctx, true)
	if err != nil {
		t.Fatalf("error listando outbox: %v", err)
	}
	if len(outboxItems) != 2 {
		t.Fatalf("se esperaban 2 mutaciones encoladas en outbox, se encontraron: %d", len(outboxItems))
	}

	for i, item := range outboxItems {
		if item.State != "pending" {
			t.Errorf("ítem %d debe estar en estado 'pending', obtenido: %s", i, item.State)
		}
		if item.Attempts != 0 {
			t.Errorf("ítem %d debe tener 0 intentos iniciales, obtenido: %d", i, item.Attempts)
		}
		if !strings.Contains(item.PayloadJSON, "PLANE_WORKLOG_METADATA") {
			t.Errorf("ítem %d no contiene PLANE_WORKLOG_METADATA en payload: %s", i, item.PayloadJSON)
		}
	}

	// =========================================================================
	// FASE 2: Caídas transitorias de Plane CE (HTTP 500, 502, 503, Desconexiones)
	// =========================================================================
	t.Log("FASE 2: Inyectando fallos transitorios de red y caos upstream...")

	// 2.1 Simulación HTTP 500 (Internal Server Error)
	chaosSrv.SetMode("500")
	rep500, err := trackerSvc.FlushOutbox(ctx, 10)
	if err != nil {
		t.Fatalf("FlushOutbox no debe propagar panic ni error fatal: %v", err)
	}
	if rep500.FailedComments != 2 || rep500.SentComments != 0 {
		t.Errorf("HTTP 500: se esperaban 2 comentarios fallidos, obtenido: %+v", rep500)
	}

	itemsAfter500, _ := store.ListOutbox(ctx, true)
	for i, it := range itemsAfter500 {
		if it.Attempts != 1 {
			t.Errorf("ítem %d debió incrementar attempts a 1 tras HTTP 500, obtenido: %d", i, it.Attempts)
		}
		if it.State != "failed" {
			t.Errorf("ítem %d debió transicionar a 'failed', obtenido: %s", i, it.State)
		}
		if !strings.Contains(it.LastError, "500") {
			t.Errorf("ítem %d debió registrar last_error con código 500, obtenido: %s", i, it.LastError)
		}
	}

	// 2.2 Simulación HTTP 502 (Bad Gateway)
	chaosSrv.SetMode("502")
	rep502, _ := trackerSvc.FlushOutbox(ctx, 10)
	if rep502.FailedComments != 2 || rep502.SentComments != 0 {
		t.Errorf("HTTP 502: se esperaban 2 fallos, obtenido: %+v", rep502)
	}

	itemsAfter502, _ := store.ListOutbox(ctx, true)
	for i, it := range itemsAfter502 {
		if it.Attempts != 2 {
			t.Errorf("ítem %d debió tener attempts=2 tras HTTP 502, obtenido: %d", i, it.Attempts)
		}
		if !strings.Contains(it.LastError, "502") {
			t.Errorf("ítem %d debió registrar last_error con código 502, obtenido: %s", i, it.LastError)
		}
	}

	// 2.3 Simulación HTTP 503 (Service Unavailable)
	chaosSrv.SetMode("503")
	rep503, _ := trackerSvc.FlushOutbox(ctx, 10)
	if rep503.FailedComments != 2 || rep503.SentComments != 0 {
		t.Errorf("HTTP 503: se esperaban 2 fallos, obtenido: %+v", rep503)
	}

	itemsAfter503, _ := store.ListOutbox(ctx, true)
	for i, it := range itemsAfter503 {
		if it.Attempts != 3 {
			t.Errorf("ítem %d debió tener attempts=3 tras HTTP 503, obtenido: %d", i, it.Attempts)
		}
		if !strings.Contains(it.LastError, "503") {
			t.Errorf("ítem %d debió registrar last_error con código 503, obtenido: %s", i, it.LastError)
		}
	}

	// 2.4 Simulación de Desconexión de Red Abrupta (TCP Reset)
	chaosSrv.SetMode("disconnect")
	repDisc, _ := trackerSvc.FlushOutbox(ctx, 10)
	if repDisc.FailedComments != 2 {
		t.Errorf("Disconnect: se esperaban 2 fallos por desconexión, obtenido: %+v", repDisc)
	}

	itemsAfterDisc, _ := store.ListOutbox(ctx, true)
	for i, it := range itemsAfterDisc {
		if it.Attempts != 4 {
			t.Errorf("ítem %d debió incrementar attempts a 4 tras disconnect, obtenido: %d", i, it.Attempts)
		}
	}

	// Verificar que los ítems NO se han perdido en SQLite
	pendingItems, _ := store.ListOutbox(ctx, false)
	if len(pendingItems) != 2 {
		t.Fatalf("los ítems pendientes/fallidos se perdieron en SQLite: count=%d", len(pendingItems))
	}

	// =========================================================================
	// FASE 3: Recuperación de Red y Drenaje Exitoso
	// =========================================================================
	t.Log("FASE 3: Restaurando conectividad y drenando outbox...")

	chaosSrv.SetMode("normal")
	repRecover, err := trackerSvc.FlushOutbox(ctx, 10)
	if err != nil {
		t.Fatalf("FlushOutbox falló tras reconectar upstream: %v", err)
	}
	if repRecover.SentComments != 2 || repRecover.FailedComments != 0 {
		t.Fatalf("se esperaba entrega total de 2 comentarios, obtenido: %+v", repRecover)
	}

	// Verificar que la outbox pasó a estado 'sent' con metadatos completos
	sentItems, err := store.ListOutbox(ctx, true)
	if err != nil {
		t.Fatalf("error listando outbox final: %v", err)
	}
	for i, it := range sentItems {
		if it.State != "sent" {
			t.Errorf("ítem %d debió marcarse como 'sent', obtenido: %s", i, it.State)
		}
		if it.PlaneCommentID == "" {
			t.Errorf("ítem %d debió recibir un plane_comment_id remoto válido", i)
		}
		if it.SentAt == "" {
			t.Errorf("ítem %d debió recibir un timestamp sent_at", i)
		}
		if it.Attempts != 5 {
			t.Errorf("ítem %d debió registrar 5 intentos acumulados (4 fallidos + 1 exitoso), obtenido: %d", i, it.Attempts)
		}
	}

	// Verificar vinculación en la tabla worklogs del ledger
	worklogs, err := store.GetTicketWorklogs(ctx, "OMNIFY-101")
	if err != nil {
		t.Fatalf("error obteniendo worklogs del ticket: %v", err)
	}
	if len(worklogs) == 0 {
		t.Fatalf("no se encontraron registros de worklog en SQLite para OMNIFY-101")
	}
	for _, wl := range worklogs {
		if wl.PlaneCommentID == "" {
			t.Errorf("el worklog en SQLite no vinculó el plane_comment_id tras la sincronización de outbox: %+v", wl)
		}
	}

	// =========================================================================
	// FASE 4: Idempotencia ante Drenajes Redundantes y Reintentos
	// =========================================================================
	t.Log("FASE 4: Verificando idempotencia estricta...")

	// 4.1 Un segundo FlushOutbox sobre cola limpia no debe reenviar nada
	commentsBefore := chaosSrv.GetCommentsCount()
	repRedundant, err := trackerSvc.FlushOutbox(ctx, 10)
	if err != nil {
		t.Fatalf("FlushOutbox redundante falló: %v", err)
	}
	if repRedundant.SentComments != 0 || repRedundant.FailedComments != 0 {
		t.Errorf("FlushOutbox redundante no fue idempotente: %+v", repRedundant)
	}
	commentsAfter := chaosSrv.GetCommentsCount()
	if commentsBefore != commentsAfter {
		t.Errorf("se enviaron comentarios duplicados al tracker remoto: antes=%d, despues=%d", commentsBefore, commentsAfter)
	}

	// 4.2 Simular respuesta ambigua (el server recibe el payload pero la conexión se corta antes del HTTP 200)
	t.Log("FASE 4.2: Probando reconciliación ante partición ambigua...")
	ambiguousPayload, _ := json.Marshal(map[string]string{
		"comment_html": "<p>Mensaje con ACK ambiguo</p>",
	})
	_ = store.EnqueueOutboxWithMutation(ctx, "client-ambiguous-1", "OMNIFY-101", string(ambiguousPayload), "", "comment")

	chaosSrv.SetMode("ambiguous")
	repAmb, _ := trackerSvc.FlushOutbox(ctx, 10)
	if repAmb.FailedComments != 1 {
		t.Errorf("se esperaba que el envío ambiguo registrara 1 fallo en el cliente: %+v", repAmb)
	}

	// Ahora restauramos el canal normal y reintentamos: debe converger sin corromper la cola
	chaosSrv.SetMode("normal")
	repReattempt, err := trackerSvc.FlushOutbox(ctx, 10)
	if err != nil || repReattempt.SentComments != 1 {
		t.Fatalf("reintento tras respuesta ambigua falló: rep=%+v, err=%v", repReattempt, err)
	}

	// Verificar que el estado final de la cola esté completamente limpio
	pendingEnd, _ := store.ListOutbox(ctx, false)
	if len(pendingEnd) != 0 {
		t.Errorf("quedaron mutaciones pendientes en la cola outbox: %+v", pendingEnd)
	}

	t.Log("TestE2EAdversarial_OutboxLifecycle_UnderChaos completado con 100% de éxito.")
}

// TestE2EAdversarial_DeltaSync_WatermarkPagingAndCutoff prueba exhaustivamente los mecanismos
// de terminación temprana y sincronización delta incremental:
// 1. Aborto de paginación exactamente cuando un issue tiene updated_at <= server_high_watermark en modo estándar.
// 2. Comportamiento en modo --deep: se ignora el cutoff y se agotan todas las páginas del cursor.
// 3. Aislamiento estricto de marcas de agua entre múltiples proyectos (OMNIFY vs OPS).
// 4. Tolerancia y resiliencia ante marcas de agua corruptas, vacías y en el futuro.
func TestE2EAdversarial_DeltaSync_WatermarkPagingAndCutoff(t *testing.T) {
	ctx := context.Background()

	// Base de datos SQLite real para aislar watermarks
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "watermarks_e2e.db")
	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("error creando SQLite store: %v", err)
	}
	defer store.Close()

	// Dataset ordenado por -updated_at
	issuesDataset := []map[string]interface{}{
		{
			"id":          "iss-uuid-1",
			"sequence_id": float64(10),
			"name":        "Issue 10 (Más reciente)",
			"updated_at":  "2026-11-16T16:00:00Z",
			"state":       "st-backlog",
		},
		{
			"id":          "iss-uuid-2",
			"sequence_id": float64(9),
			"name":        "Issue 9",
			"updated_at":  "2026-11-16T15:00:00Z",
			"state":       "st-backlog",
		},
		{
			"id":          "iss-uuid-3",
			"sequence_id": float64(8),
			"name":        "Issue 8 (Exacto a Marca de Agua Previa)",
			"updated_at":  "2026-11-16T14:00:00Z",
			"state":       "st-backlog",
		},
		{
			"id":          "iss-uuid-4",
			"sequence_id": float64(7),
			"name":        "Issue 7 (Antiguo)",
			"updated_at":  "2026-11-16T13:00:00Z",
			"state":       "st-backlog",
		},
		{
			"id":          "iss-uuid-5",
			"sequence_id": float64(6),
			"name":        "Issue 6 (Más antiguo)",
			"updated_at":  "2026-11-16T12:00:00Z",
			"state":       "st-backlog",
		},
	}

	chaosSrv := newMockChaosServer()
	chaosSrv.SetIssues(issuesDataset)
	httpSrv := httptest.NewServer(chaosSrv)
	defer httpSrv.Close()

	planeClient := tracker_plane.NewClient(tracker_plane.Config{
		BaseURL:   httpSrv.URL,
		APIKey:    "test-api-key",
		Workspace: "test-ws",
		Timeout:   2 * time.Second,
	})

	// -------------------------------------------------------------------------
	// 1. Terminación Temprana Exacta en modo estándar (deep = false)
	// -------------------------------------------------------------------------
	t.Run("EarlyTerminationExact_StandardMode", func(t *testing.T) {
		cutoff := "2026-11-16T14:00:00Z" // Igual al updated_at de Issue 8
		snaps, newest, err := planeClient.FetchIssuesDelta(ctx, "proj-omnify", "OMNIFY", 100, cutoff, false)
		if err != nil {
			t.Fatalf("FetchIssuesDelta falló: %v", err)
		}

		// Debe haberse detenido exactamente en Issue 8, descargando únicamente Issue 10 y Issue 9
		if len(snaps) != 2 {
			t.Fatalf("se esperaban exactamente 2 tickets delta, se obtuvieron %d", len(snaps))
		}
		if snaps[0].Item.SequenceID != 10 || snaps[1].Item.SequenceID != 9 {
			t.Errorf("los tickets descargados no corresponden a los deltas esperados: %+v", snaps)
		}
		if newest != "2026-11-16T16:00:00Z" {
			t.Errorf("la marca más reciente reportada debió ser 2026-11-16T16:00:00Z, obtenido: %s", newest)
		}
	})

	// -------------------------------------------------------------------------
	// 2. Modo --deep (deep = true): Ignora Cutoff y Agota Cursor de Paginación
	// -------------------------------------------------------------------------
	t.Run("DeepMode_CursorExhaustion", func(t *testing.T) {
		cutoff := "2026-11-16T14:00:00Z"
		// En modo deep, a pesar de que Issue 8 <= cutoff, debe continuar paginando hasta agotar los 5 tickets
		snaps, newest, err := planeClient.FetchIssuesDelta(ctx, "proj-omnify", "OMNIFY", 2, cutoff, true)
		if err != nil {
			t.Fatalf("FetchIssuesDelta con deep=true falló: %v", err)
		}

		if len(snaps) != 5 {
			t.Fatalf("en modo deep debieron descargarse los 5 tickets completos mediante paginación, obtenido: %d", len(snaps))
		}
		if newest != "2026-11-16T16:00:00Z" {
			t.Errorf("newest esperado 2026-11-16T16:00:00Z, obtenido: %s", newest)
		}
	})

	// -------------------------------------------------------------------------
	// 3. Aislamiento Estricto de Marcas de Agua entre Múltiples Proyectos
	// -------------------------------------------------------------------------
	t.Run("MultiProject_WatermarkIsolation", func(t *testing.T) {
		// Asignar marcas de agua distintas para OMNIFY y OPS
		wmOmnify := issue.SyncWatermark{
			Domain:              "issues",
			ProjectID:           "proj-omnify",
			ServerHighWatermark: "2026-11-16T22:00:00Z",
			Status:              "synced",
			ItemsSynced:         15,
			PagesFetched:        1,
			DurationMs:          120,
			UpdatedAt:           time.Now(),
		}
		wmOps := issue.SyncWatermark{
			Domain:              "issues",
			ProjectID:           "proj-ops",
			ServerHighWatermark: "2026-11-10T08:00:00Z",
			Status:              "synced",
			ItemsSynced:         4,
			PagesFetched:        1,
			DurationMs:          85,
			UpdatedAt:           time.Now(),
		}

		if err := store.SetWatermark(ctx, wmOmnify); err != nil {
			t.Fatalf("error guardando watermark de OMNIFY: %v", err)
		}
		if err := store.SetWatermark(ctx, wmOps); err != nil {
			t.Fatalf("error guardando watermark de OPS: %v", err)
		}

		// Leer de vuelta y verificar aislamiento estricto
		readOmnify, err := store.GetWatermark(ctx, "issues", "proj-omnify")
		if err != nil || readOmnify == nil {
			t.Fatalf("error leyendo watermark OMNIFY: %v", err)
		}
		readOps, err := store.GetWatermark(ctx, "issues", "proj-ops")
		if err != nil || readOps == nil {
			t.Fatalf("error leyendo watermark OPS: %v", err)
		}

		if readOmnify.ServerHighWatermark != "2026-11-16T22:00:00Z" {
			t.Errorf("watermark OMNIFY alterado: %s", readOmnify.ServerHighWatermark)
		}
		if readOps.ServerHighWatermark != "2026-11-10T08:00:00Z" {
			t.Errorf("watermark OPS alterado: %s", readOps.ServerHighWatermark)
		}

		// Modificar el watermark de OPS y asegurar que OMNIFY no cambia
		wmOps.ServerHighWatermark = "2026-11-11T09:00:00Z"
		_ = store.SetWatermark(ctx, wmOps)

		readOmnifyAfter, _ := store.GetWatermark(ctx, "issues", "proj-omnify")
		if readOmnifyAfter.ServerHighWatermark != "2026-11-16T22:00:00Z" {
			t.Errorf("watermark OMNIFY fue contaminado por actualización en OPS: %s", readOmnifyAfter.ServerHighWatermark)
		}
	})

	// -------------------------------------------------------------------------
	// 4. Resistencia ante Watermarks Corruptos, Vacíos o en el Futuro
	// -------------------------------------------------------------------------
	t.Run("CorruptedAndFuture_WatermarkResilience", func(t *testing.T) {
		// 4.1 Watermark Vacío (""): sincronización inicial normal sin parada prematura
		snapsEmpty, _, err := planeClient.FetchIssuesDelta(ctx, "proj-omnify", "OMNIFY", 100, "", false)
		if err != nil {
			t.Fatalf("error con watermark vacío: %v", err)
		}
		if len(snapsEmpty) == 0 {
			t.Errorf("watermark vacío debió descargar los tickets disponibles")
		}

		// 4.2 Watermark Corrupto ("cadena_invalida_no_iso", "!@#$%^&*")
		snapsCorrupt, _, err := planeClient.FetchIssuesDelta(ctx, "proj-omnify", "OMNIFY", 100, "malformed-timestamp-###", false)
		if err != nil {
			t.Fatalf("error al procesar watermark corrupto: %v", err)
		}
		if len(snapsCorrupt) == 0 {
			t.Errorf("watermark corrupto no debe causar crash ni abortar indebidamente")
		}

		// 4.3 Watermark en el Futuro ("2099-01-01T00:00:00Z")
		// En modo estándar, como el primer issue tiene updated_at <= fecha_futura, aborta limpiamente sin error
		snapsFuture, _, err := planeClient.FetchIssuesDelta(ctx, "proj-omnify", "OMNIFY", 100, "2099-01-01T00:00:00Z", false)
		if err != nil {
			t.Fatalf("error con watermark futuro: %v", err)
		}
		if len(snapsFuture) != 0 {
			t.Errorf("con marca de agua en el futuro, el modo estándar debe detenerse en 0 deltas, obtenido: %d", len(snapsFuture))
		}

		// Con deep=true, debe ignorar la fecha futura y descargar todos los datos
		snapsFutureDeep, _, err := planeClient.FetchIssuesDelta(ctx, "proj-omnify", "OMNIFY", 2, "2099-01-01T00:00:00Z", true)
		if err != nil {
			t.Fatalf("error con watermark futuro en modo deep: %v", err)
		}
		if len(snapsFutureDeep) != 5 {
			t.Errorf("modo deep con watermark futuro debió descargar los 5 tickets, obtenido: %d", len(snapsFutureDeep))
		}
	})

	t.Log("TestE2EAdversarial_DeltaSync_WatermarkPagingAndCutoff completado con 100% de éxito.")
}

// mockConcurrentTokenClient emula un backend remoto para la prueba de contención WAL.
type mockConcurrentTokenClient struct {
	mu           sync.Mutex
	commentCount int64
}

func (m *mockConcurrentTokenClient) Whoami(ctx context.Context) (*issue.UserInfo, error) {
	return &issue.UserInfo{ID: "usr-stress", Email: "stress@allostasis.dev", DisplayName: "Stress Worker"}, nil
}
func (m *mockConcurrentTokenClient) FetchProjects(ctx context.Context) ([]issue.ProjectRecord, error) {
	return []issue.ProjectRecord{
		{Identifier: "OMNIFY", ProjectID: "proj-omnify", Name: "Omnify Core"},
	}, nil
}
func (m *mockConcurrentTokenClient) FetchStates(ctx context.Context, projectID string) ([]issue.StateRecord, error) {
	return []issue.StateRecord{
		{ID: "st-backlog", ProjectID: projectID, Name: "Backlog", Slug: "backlog", GroupName: "backlog"},
	}, nil
}
func (m *mockConcurrentTokenClient) FetchLabels(ctx context.Context, projectID string) ([]issue.Label, error) {
	return []issue.Label{{ID: "lbl-1", ProjectID: projectID, Name: "stress"}}, nil
}
func (m *mockConcurrentTokenClient) FetchIssues(ctx context.Context, projectID, projectKey string, limit int) ([]issue.IssueSnapshot, error) {
	return []issue.IssueSnapshot{}, nil
}
func (m *mockConcurrentTokenClient) FetchIssuesDelta(ctx context.Context, projectID, projectKey string, limit int, cutoffISO string, deep bool) ([]issue.IssueSnapshot, string, error) {
	return []issue.IssueSnapshot{}, "", nil
}
func (m *mockConcurrentTokenClient) CreateIssueRemote(ctx context.Context, projectID string, input issue.CreateIssueInput, stateID string) (*issue.WorkItem, error) {
	return &issue.WorkItem{ID: "remote-iss-1", Key: input.ProjectKey + "-100", Title: input.Title}, nil
}
func (m *mockConcurrentTokenClient) UpdateIssueRemote(ctx context.Context, projectID, issueID string, input issue.UpdateIssueInput, stateID string) error {
	return nil
}
func (m *mockConcurrentTokenClient) DeleteIssueRemote(ctx context.Context, projectID, issueID string) error {
	return nil
}
func (m *mockConcurrentTokenClient) FetchComments(ctx context.Context, projectID, issueID string) ([]issue.CommentRecord, error) {
	return []issue.CommentRecord{}, nil
}
func (m *mockConcurrentTokenClient) SendCommentRaw(ctx context.Context, targetWS, projectID, issueID, payloadJSON string) (string, error) {
	m.mu.Lock()
	m.commentCount++
	id := fmt.Sprintf("cmt-stress-%d", m.commentCount)
	m.mu.Unlock()
	return id, nil
}
func (m *mockConcurrentTokenClient) EditCommentRemote(ctx context.Context, projectID, issueID, commentID, newBody string) error {
	return nil
}
func (m *mockConcurrentTokenClient) DeleteCommentRemote(ctx context.Context, projectID, issueID, commentID string) error {
	return nil
}
func (m *mockConcurrentTokenClient) AddLinkRemote(ctx context.Context, projectID, issueID, title, linkURL string) (*issue.Link, error) {
	return &issue.Link{ID: "lnk-1", IssueID: issueID, Title: title, URL: linkURL}, nil
}
func (m *mockConcurrentTokenClient) ListLinksRemote(ctx context.Context, projectID, issueID string) ([]issue.Link, error) {
	return nil, nil
}
func (m *mockConcurrentTokenClient) DeleteLinkRemote(ctx context.Context, projectID, issueID, linkID string) error {
	return nil
}

func (m *mockConcurrentTokenClient) UploadAttachmentRemote(ctx context.Context, projectID, issueID string, up issue.AttachmentUpload) (*issue.Attachment, error) {
	return &issue.Attachment{ID: "att-1", IssueID: issueID, Name: up.Name, SizeBytes: up.Size}, nil
}
func (m *mockConcurrentTokenClient) ListAttachmentsRemote(ctx context.Context, projectID, issueID string) ([]issue.Attachment, bool, error) {
	return nil, true, nil
}
func (m *mockConcurrentTokenClient) DeleteAttachmentRemote(ctx context.Context, projectID, issueID, attachmentID string) error {
	return nil
}

// TestE2EAdversarial_WALDoublePool_ExtremeConcurrency somete el motor SQLite
// con arquitectura WAL Double-Pool (lector multiconexión y escritor único con busy_timeout)
// a contención masiva con 40 goroutines simultáneas ejecutando:
// - Lecturas complejas filtradas (ListIssuesFiltered con --involved, --mine, --created).
// - Creación de comentarios (AddComment).
// - Registro telemétrico de horas de trabajo (LogTime).
// - Drenaje concurrente de outbox (FlushOutbox).
// - Actualizaciones de marcas de agua (SetWatermark).
// Verifica: 0 carreras de datos (-race), 0 bloqueos ('database is locked') e integridad 100% limpia.
func TestE2EAdversarial_WALDoublePool_ExtremeConcurrency(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "wal_extreme_stress.db")

	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("error inicializando SQLite en disco con WAL: %v", err)
	}
	defer store.Close()

	// 1. Precargar catálogo base e issues iniciales
	_ = store.SaveProject(ctx, issue.ProjectRecord{Identifier: "OMNIFY", ProjectID: "proj-omnify", Name: "Omnify Core"})
	_ = store.SaveProject(ctx, issue.ProjectRecord{Identifier: "OPS", ProjectID: "proj-ops", Name: "Operations"})

	const initialTickets = 50
	for i := 1; i <= initialTickets; i++ {
		key := fmt.Sprintf("OMNIFY-%d", i)
		assignee := "usr-dev"
		if i%2 == 0 {
			assignee = "usr-lead"
		}
		item := &issue.WorkItem{
			ID:         fmt.Sprintf("iss-uuid-%d", i),
			Key:        key,
			ProjectID:  "proj-omnify",
			ProjectKey: "OMNIFY",
			SequenceID: i,
			Title:      fmt.Sprintf("Tarea de prueba concurrente %d", i),
			StateName:  "Backlog",
			StateGroup: "backlog",
			State:      issue.StateBacklog,
			Priority:   "normal",
			Assignee:   assignee,
			Assignees:  []string{assignee},
			CreatedBy:  "usr-lead",
			CreatedAt:  time.Now().Add(-time.Duration(i) * time.Hour),
			UpdatedAt:  time.Now(),
		}
		if err := store.SaveIssue(ctx, item); err != nil {
			t.Fatalf("error precargando ticket %s: %v", key, err)
		}
	}

	remoteClient := &mockConcurrentTokenClient{}
	trackerSvc := app.NewTrackerService(store, remoteClient)
	cockpitSvc := app.NewCockpitService(store, store, nil, clock.NewRealClock())

	// 2. Parámetros de concurrencia masiva
	const (
		numWorkers          = 40
		iterationsPerWorker = 30
	)

	var (
		wg             sync.WaitGroup
		startBarrier   = make(chan struct{})
		lockedErrors   int64
		readErrors     int64
		commentErrors  int64
		logTimeErrors  int64
		flushErrors    int64
		watermarkErrs  int64
		totalReadOps   int64
		totalWriteOps  int64
	)

	// 3. Despliegue de los 40 trabajadores concurrentes
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-startBarrier // Esperar disparo sincronizado simultáneo

			for it := 0; it < iterationsPerWorker; it++ {
				role := workerID % 5
				ticketNum := ((workerID*iterationsPerWorker + it) % initialTickets) + 1
				ticketKey := fmt.Sprintf("OMNIFY-%d", ticketNum)

				switch role {
				case 0, 1:
					// Lecturas complejas con subqueries sobre el pool de lectores (rDB)
					atomic.AddInt64(&totalReadOps, 1)
					var listErr error

					if it%3 == 0 {
						// Filtro --involved (subqueries contra worklogs y assignment_events)
						_, listErr = store.ListIssuesFiltered(ctx, issue.IssueFilter{
							InvolvedBy: "usr-lead",
							Limit:      20,
						})
					} else if it%3 == 1 {
						// Filtro --mine
						_, listErr = store.ListIssuesFiltered(ctx, issue.IssueFilter{
							Assignee: "usr-dev",
							Limit:    20,
						})
					} else {
						// Filtro --created
						_, listErr = store.ListIssuesFiltered(ctx, issue.IssueFilter{
							CreatedBy: "usr-lead",
							Limit:     20,
						})
					}

					if listErr != nil {
						atomic.AddInt64(&readErrors, 1)
						if strings.Contains(listErr.Error(), "database is locked") || strings.Contains(listErr.Error(), "busy") {
							atomic.AddInt64(&lockedErrors, 1)
						}
					}

					// Lectura de watermark
					_, _ = store.GetWatermark(ctx, "issues", "proj-omnify")

				case 2:
					// Creación de comentarios sobre el pool de escritura (wDB)
					atomic.AddInt64(&totalWriteOps, 1)
					_, err := trackerSvc.AddComment(ctx, ticketKey, fmt.Sprintf("Comentario de worker %d iter %d", workerID, it), 0, "", "")
					if err != nil {
						atomic.AddInt64(&commentErrors, 1)
						if strings.Contains(err.Error(), "database is locked") || strings.Contains(err.Error(), "busy") {
							atomic.AddInt64(&lockedErrors, 1)
						}
					}

				case 3:
					// Registro telemétrico de horas de trabajo (LogTime en wDB)
					atomic.AddInt64(&totalWriteOps, 1)
					err := cockpitSvc.LogTime(ctx, ticketKey, 15, ledger.BucketDeepWork, fmt.Sprintf("Sesión concurrente worker %d iter %d", workerID, it))
					if err != nil {
						atomic.AddInt64(&logTimeErrors, 1)
						if strings.Contains(err.Error(), "database is locked") || strings.Contains(err.Error(), "busy") {
							atomic.AddInt64(&lockedErrors, 1)
						}
					}

				case 4:
					// Drenaje de Outbox y Actualizaciones de Watermark
					atomic.AddInt64(&totalWriteOps, 1)
					if it%2 == 0 {
						_, err := trackerSvc.FlushOutbox(ctx, 5)
						if err != nil {
							atomic.AddInt64(&flushErrors, 1)
							if strings.Contains(err.Error(), "database is locked") || strings.Contains(err.Error(), "busy") {
								atomic.AddInt64(&lockedErrors, 1)
							}
						}
					} else {
						wm := issue.SyncWatermark{
							Domain:              "issues",
							ProjectID:           "proj-omnify",
							ServerHighWatermark: fmt.Sprintf("2026-11-16T%02d:%02d:00Z", it%24, (workerID*7)%60),
							Status:              "synced",
							ItemsSynced:         it,
							PagesFetched:        1,
							DurationMs:          int64(it * 10),
							UpdatedAt:           time.Now(),
						}
						if err := store.SetWatermark(ctx, wm); err != nil {
							atomic.AddInt64(&watermarkErrs, 1)
							if strings.Contains(err.Error(), "database is locked") || strings.Contains(err.Error(), "busy") {
								atomic.AddInt64(&lockedErrors, 1)
							}
						}
					}
				}
			}
		}(w)
	}

	// 4. Iniciar carrera concurrente y esperar terminación
	startT := time.Now()
	close(startBarrier)
	wg.Wait()
	duration := time.Since(startT)

	t.Logf("Carga concurrente completada en %v. Operaciones: lecturas=%d, escrituras=%d", duration, totalReadOps, totalWriteOps)
	t.Logf("Errores registrados: locked=%d, readErrs=%d, cmtErrs=%d, logErrs=%d, flushErrs=%d, wmErrs=%d",
		lockedErrors, readErrors, commentErrors, logTimeErrors, flushErrors, watermarkErrs)

	// 5. Validaciones adversariales estrictas
	if lockedErrors > 0 {
		t.Fatalf("FALLO CRÍTICO DE CONCURRENCIA: Se detectaron %d errores de 'database is locked'", lockedErrors)
	}
	if readErrors > 0 {
		t.Errorf("se registraron %d errores en lecturas concurrentes", readErrors)
	}
	if commentErrors > 0 {
		t.Errorf("se registraron %d errores al añadir comentarios concurrentes", commentErrors)
	}
	if logTimeErrors > 0 {
		t.Errorf("se registraron %d errores en LogTime concurrente", logTimeErrors)
	}
	if flushErrors > 0 {
		t.Errorf("se registraron %d errores en FlushOutbox concurrente", flushErrors)
	}
	if watermarkErrs > 0 {
		t.Errorf("se registraron %d errores en SetWatermark concurrente", watermarkErrs)
	}

	// 6. Verificación de Integridad Estricta de SQLite (PRAGMA integrity_check)
	var integrityResult string
	row := store.DB().QueryRowContext(ctx, "PRAGMA integrity_check")
	if err := row.Scan(&integrityResult); err != nil {
		t.Fatalf("error ejecutando PRAGMA integrity_check: %v", err)
	}
	if integrityResult != "ok" {
		t.Fatalf("CORRUPCIÓN DETECTADA en base de datos SQLite tras estrés WAL: resultado=%s", integrityResult)
	}

	// 7. Drenaje final del outbox para validar consistencia residual
	finalFlush, err := trackerSvc.FlushOutbox(ctx, 1000)
	if err != nil {
		t.Fatalf("error en drenaje final de outbox: %v", err)
	}
	t.Logf("Drenaje final completado: sent=%d, failed=%d", finalFlush.SentComments, finalFlush.FailedComments)

	pendingResidual, err := store.ListOutbox(ctx, false)
	if err != nil {
		t.Fatalf("error consultando outbox residual: %v", err)
	}
	if len(pendingResidual) > 0 {
		t.Errorf("quedaron %d registros pendientes no reconciliados en outbox", len(pendingResidual))
	}

	t.Log("TestE2EAdversarial_WALDoublePool_ExtremeConcurrency completado con 100% de éxito.")
}

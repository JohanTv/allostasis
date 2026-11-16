package tracker_plane_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/allostasis/alo/internal/adapters/tracker_plane"
	"github.com/allostasis/alo/internal/domain/issue"
)

// TestAdversarial_NetworkAndProtocolFailures somete al cliente HTTP a condiciones de red hostiles:
// timeouts abruptos, cancelaciones de contexto, proxies con errores 502/503/504,
// streams truncados a mitad de transmisión y respuestas con JSON corrupto.
func TestAdversarial_NetworkAndProtocolFailures(t *testing.T) {
	t.Run("TimeoutAbrupto", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simular latencia excesiva que supera el timeout del cliente
			time.Sleep(150 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"results":[]}`))
		}))
		defer server.Close()

		cfg := tracker_plane.Config{
			BaseURL:   server.URL,
			APIKey:    "test-key",
			Workspace: "test-workspace",
			Timeout:   30 * time.Millisecond, // Timeout agresivo
		}
		client := tracker_plane.NewClient(cfg)

		_, err := client.FetchProjects(context.Background())
		if err == nil {
			t.Fatal("se esperaba error por timeout pero la petición tuvo éxito")
		}
		if !strings.Contains(err.Error(), "Client.Timeout") && !strings.Contains(err.Error(), "deadline exceeded") {
			t.Logf("Error retornado por timeout: %v", err)
		}
	})

	t.Run("ContextoCanceled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := tracker_plane.Config{
			BaseURL:   server.URL,
			APIKey:    "test-key",
			Workspace: "test-workspace",
			Timeout:   2 * time.Second,
		}
		client := tracker_plane.NewClient(cfg)

		ctx, cancel := context.WithCancel(context.Background())
		// Cancelar inmediatamente antes de completar
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		_, err := client.Whoami(ctx)
		if err == nil {
			t.Fatal("se esperaba error por cancelación de contexto")
		}
		if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("error no envuelve context.Canceled: %v", err)
		}
	})

	t.Run("ProxyErrors_502_503_504", func(t *testing.T) {
		statusCodes := []struct {
			code int
			body string
		}{
			{http.StatusBadGateway, "502 Bad Gateway - Upstream timeout in Nginx"},
			{http.StatusServiceUnavailable, "503 Service Unavailable - Plane worker overloaded"},
			{http.StatusGatewayTimeout, "504 Gateway Timeout - Cloudflare tunnel timeout"},
		}

		for _, tc := range statusCodes {
			t.Run(fmt.Sprintf("HTTP_%d", tc.code), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tc.code)
					_, _ = w.Write([]byte(tc.body))
				}))
				defer server.Close()

				cfg := tracker_plane.Config{
					BaseURL:   server.URL,
					APIKey:    "test-key",
					Workspace: "test-workspace",
					Timeout:   1 * time.Second,
				}
				client := tracker_plane.NewClient(cfg)
				ctx := context.Background()

				// 1. FetchProjects
				_, err := client.FetchProjects(ctx)
				if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%d", tc.code)) {
					t.Errorf("FetchProjects: esperado error con código %d, obtuvo: %v", tc.code, err)
				}

				// 2. FetchIssues
				_, err = client.FetchIssues(ctx, "proj-1", "OMNIFY", 10)
				if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%d", tc.code)) {
					t.Errorf("FetchIssues: esperado error con código %d, obtuvo: %v", tc.code, err)
				}

				// 3. SendCommentRaw
				_, err = client.SendCommentRaw(ctx, "test-workspace", "proj-1", "iss-1", `{"comment_html":"<p>test</p>"}`)
				if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%d", tc.code)) {
					t.Errorf("SendCommentRaw: esperado error con código %d, obtuvo: %v", tc.code, err)
				}

				// 4. CreateIssueRemote
				_, err = client.CreateIssueRemote(ctx, "proj-1", issue.CreateIssueInput{ProjectKey: "OMNIFY", Title: "Test"}, "")
				if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%d", tc.code)) {
					t.Errorf("CreateIssueRemote: esperado error con código %d, obtuvo: %v", tc.code, err)
				}
			})
		}
	})

	t.Run("PayloadTruncado_AbruptEOF", func(t *testing.T) {
		// Simular corte abrupto de conexión a mitad de respuesta HTTP mediante Hijacker
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "hijacking not supported", http.StatusInternalServerError)
				return
			}
			conn, buf, err := hj.Hijack()
			if err != nil {
				return
			}
			defer conn.Close()

			// Enviar cabeceras HTTP anunciando longitud grande pero cortar intempestivamente
			_, _ = buf.WriteString("HTTP/1.1 200 OK\r\n")
			_, _ = buf.WriteString("Content-Type: application/json\r\n")
			_, _ = buf.WriteString("Content-Length: 5000\r\n\r\n")
			_, _ = buf.WriteString(`{"results":[{"id":"iss-incomplete","title":"Trun`)
			_ = buf.Flush()
			// Cerrar conexión TCP forzosamente enviando TCP RST/FIN
			_ = conn.(*net.TCPConn).SetLinger(0)
		}))
		defer server.Close()

		cfg := tracker_plane.Config{
			BaseURL:   server.URL,
			APIKey:    "test-key",
			Workspace: "test-workspace",
			Timeout:   1 * time.Second,
		}
		client := tracker_plane.NewClient(cfg)
		ctx := context.Background()

		_, err := client.FetchIssues(ctx, "proj-1", "OMNIFY", 10)
		if err == nil {
			t.Fatal("se esperaba error por EOF abrupto / payload truncado, pero retornó nil")
		}
		t.Logf("Error capturado con éxito por payload truncado: %v", err)
	})

	t.Run("JSONCorrupto_Malformado", func(t *testing.T) {
		corruptBodies := []string{
			`{"results": [{"id": "foo", "sequence_id": ]}`,
			`<html><body><h1>500 Internal Error</h1></body></html>`,
			`{"results": [ { unquoted_key: 123 } ] }`,
			`<<<NOT_A_JSON>>>`,
		}

		for idx, body := range corruptBodies {
			t.Run(fmt.Sprintf("Payload_%d", idx), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(body))
				}))
				defer server.Close()

				cfg := tracker_plane.Config{
					BaseURL:   server.URL,
					APIKey:    "test-key",
					Workspace: "test-workspace",
					Timeout:   1 * time.Second,
				}
				client := tracker_plane.NewClient(cfg)
				ctx := context.Background()

				_, err := client.FetchIssues(ctx, "proj-1", "OMNIFY", 10)
				if err == nil {
					t.Fatalf("se esperaba error al parsear JSON corrupto en caso %d", idx)
				}
			})
		}
	})
}

// TestAdversarial_ChaoticPlanePayloads valida que el cliente resista payloads deformes o inesperados
// que devuelva Plane CE (nulls, tipos primitivos en vez de mapas/arrays, UTF-8 extremo, etc.).
func TestAdversarial_ChaoticPlanePayloads(t *testing.T) {
	t.Run("ResultsNull_Y_BodyVacio", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"results": null}`))
		}))
		defer server.Close()

		cfg := tracker_plane.Config{
			BaseURL:   server.URL,
			APIKey:    "test-key",
			Workspace: "test-workspace",
			Timeout:   1 * time.Second,
		}
		client := tracker_plane.NewClient(cfg)
		ctx := context.Background()

		issuesList, err := client.FetchIssues(ctx, "proj-1", "OMNIFY", 10)
		if err != nil {
			t.Fatalf("FetchIssues no debió fallar con results:null: %v", err)
		}
		if len(issuesList) != 0 {
			t.Errorf("se esperaban 0 issues, obtuvo %d", len(issuesList))
		}

		projects, err := client.FetchProjects(ctx)
		if err != nil {
			t.Fatalf("FetchProjects no debió fallar con results:null: %v", err)
		}
		if len(projects) != 0 {
			t.Errorf("se esperaban 0 projects, obtuvo %d", len(projects))
		}

		states, err := client.FetchStates(ctx, "proj-1")
		if err != nil {
			t.Fatalf("FetchStates no debió fallar con results:null: %v", err)
		}
		if len(states) != 0 {
			t.Errorf("se esperaban 0 states, obtuvo %d", len(states))
		}

		labels, err := client.FetchLabels(ctx, "proj-1")
		if err != nil {
			t.Fatalf("FetchLabels no debió fallar con results:null: %v", err)
		}
		if len(labels) != 0 {
			t.Errorf("se esperaban 0 labels, obtuvo %d", len(labels))
		}
	})

	t.Run("StateNull_StateDetailNull_Y_TiposInesperados", func(t *testing.T) {
		chaoticJSON := `{
			"results": [
				{
					"id": "iss-null-state",
					"sequence_id": 101,
					"name": "Tarea con state null",
					"state": null,
					"state_detail": null
				},
				{
					"id": "iss-numeric-state",
					"sequence_id": 102,
					"name": "Tarea con state numérico inesperado",
					"state": 99999,
					"state_detail": null
				},
				{
					"id": "iss-nested-map-state",
					"sequence_id": 103,
					"name": "Tarea con state como objeto directo",
					"state": {"id": "st-custom", "name": "En Análisis", "group": "started"},
					"state_detail": null
				}
			]
		}`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(chaoticJSON))
		}))
		defer server.Close()

		cfg := tracker_plane.Config{
			BaseURL:   server.URL,
			APIKey:    "test-key",
			Workspace: "test-workspace",
			Timeout:   1 * time.Second,
		}
		client := tracker_plane.NewClient(cfg)

		snapshots, err := client.FetchIssues(context.Background(), "proj-1", "OMNIFY", 10)
		if err != nil {
			t.Fatalf("FetchIssues falló ante estados caóticos: %v", err)
		}
		if len(snapshots) != 3 {
			t.Fatalf("se esperaban 3 snapshots, se obtuvieron: %d", len(snapshots))
		}

		// Validar fallbacks seguros
		if snapshots[0].Item.StateName != "Backlog" || snapshots[0].Item.StateGroup != "backlog" {
			t.Errorf("estado por defecto no asignado: %+v", snapshots[0].Item)
		}
		if snapshots[1].Item.StateName != "Backlog" {
			t.Errorf("estado no manejó fallback numérico: %+v", snapshots[1].Item)
		}
		if snapshots[2].Item.StateName != "En Análisis" || snapshots[2].Item.StateGroup != "started" {
			t.Errorf("estado como objeto no parseado correctamente: %+v", snapshots[2].Item)
		}
	})

	t.Run("AssigneesNull_Y_ElementosHeterogeneos", func(t *testing.T) {
		chaoticJSON := `{
			"results": [
				{
					"id": "iss-assignees-null",
					"sequence_id": 201,
					"name": "Tarea con assignees null",
					"assignees": null
				},
				{
					"id": "iss-assignees-heterogeneos",
					"sequence_id": 202,
					"name": "Tarea con array caótico de assignees",
					"assignees": [null, "usr-uuid-plain", {"id": "usr-uuid-nested"}, 12345, {}]
				}
			]
		}`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(chaoticJSON))
		}))
		defer server.Close()

		cfg := tracker_plane.Config{
			BaseURL:   server.URL,
			APIKey:    "test-key",
			Workspace: "test-workspace",
			Timeout:   1 * time.Second,
		}
		client := tracker_plane.NewClient(cfg)

		snapshots, err := client.FetchIssues(context.Background(), "proj-1", "OMNIFY", 10)
		if err != nil {
			t.Fatalf("FetchIssues falló ante assignees caóticos: %v", err)
		}
		if len(snapshots) != 2 {
			t.Fatalf("se esperaban 2 snapshots, se obtuvieron: %d", len(snapshots))
		}

		// Item 0: assignees vacío
		if len(snapshots[0].Item.Assignees) != 0 {
			t.Errorf("se esperaba assignees vacío para item 0, obtuvo: %v", snapshots[0].Item.Assignees)
		}

		// Item 1: deben haberse rescatado los dos IDs válidos ignorando nulls y tipos inesperados
		item1 := snapshots[1].Item
		if len(item1.Assignees) != 2 {
			t.Errorf("se esperaban exactamente 2 assignees válidos rescatados, obtuvo %d: %v", len(item1.Assignees), item1.Assignees)
		}
		if item1.Assignee != "usr-uuid-plain" {
			t.Errorf("primer asignado inesperado: %s", item1.Assignee)
		}
	})

	t.Run("SequenceID_VariacionesDeTipo", func(t *testing.T) {
		chaoticJSON := `{
			"results": [
				{"id": "iss-seq-str", "sequence_id": "301", "name": "Seq como string"},
				{"id": "iss-seq-float", "sequence_id": 302.0, "name": "Seq como float"},
				{"id": "iss-seq-null", "sequence_id": null, "name": "Seq nulo"}
			]
		}`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(chaoticJSON))
		}))
		defer server.Close()

		cfg := tracker_plane.Config{
			BaseURL:   server.URL,
			APIKey:    "test-key",
			Workspace: "test-workspace",
			Timeout:   1 * time.Second,
		}
		client := tracker_plane.NewClient(cfg)

		snapshots, err := client.FetchIssues(context.Background(), "proj-1", "OMNIFY", 10)
		if err != nil {
			t.Fatalf("FetchIssues falló ante sequence_id heterogéneo: %v", err)
		}

		if snapshots[0].Item.SequenceID != 301 || snapshots[0].Item.Key != "OMNIFY-301" {
			t.Errorf("sequence_id string no convertido: %+v", snapshots[0].Item)
		}
		if snapshots[1].Item.SequenceID != 302 || snapshots[1].Item.Key != "OMNIFY-302" {
			t.Errorf("sequence_id float no convertido: %+v", snapshots[1].Item)
		}
		if snapshots[2].Item.SequenceID != 0 {
			t.Errorf("sequence_id nulo no fue 0: %+v", snapshots[2].Item)
		}
	})

	t.Run("CaracteresUTF8_ExtremosYControl", func(t *testing.T) {
		tituloComplejo := "🚀 [FEAT] 日本語テスト & 🧠 Resiliencia — 'Comillas' \"Dobles\" <xml> & \t Tab"
		descCompleja := "اختبار اللغة العربية con emojis 🔥 y saltos de línea\r\nMultilínea segura."

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusCreated)
			resp := fmt.Sprintf(`{"id":"iss-utf8-ok","sequence_id":450,"name":%q,"description_html":%q}`, tituloComplejo, descCompleja)
			_, _ = w.Write([]byte(resp))
		}))
		defer server.Close()

		cfg := tracker_plane.Config{
			BaseURL:   server.URL,
			APIKey:    "test-key",
			Workspace: "test-workspace",
			Timeout:   1 * time.Second,
		}
		client := tracker_plane.NewClient(cfg)

		created, err := client.CreateIssueRemote(context.Background(), "proj-1", issue.CreateIssueInput{
			ProjectKey:  "OMNIFY",
			Title:       tituloComplejo,
			Description: descCompleja,
		}, "")

		if err != nil {
			t.Fatalf("CreateIssueRemote falló con caracteres complejos UTF-8: %v", err)
		}
		if created.Title != tituloComplejo {
			t.Errorf("título corrompido: esperado %q, obtenido %q", tituloComplejo, created.Title)
		}
	})
}

// TestAdversarial_StressAndRaceDetector valida que el cliente opere libre de data races
// bajo ejecución simultánea masiva de peticiones contra el backend HTTP.
func TestAdversarial_StressAndRaceDetector(t *testing.T) {
	var requestCount int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/issues/"):
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":"iss-stress","sequence_id":999,"name":"Stress task"}`))
			} else {
				_, _ = w.Write([]byte(`{"results":[{"id":"iss-1","sequence_id":1,"name":"Task 1"}]}`))
			}
		case strings.Contains(r.URL.Path, "/projects/"):
			_, _ = w.Write([]byte(`{"results":[{"id":"proj-1","identifier":"OMNIFY","name":"Omnify"}]}`))
		case strings.Contains(r.URL.Path, "/states/"):
			_, _ = w.Write([]byte(`{"results":[{"id":"st-1","name":"Backlog","slug":"backlog","group":"backlog"}]}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()

	cfg := tracker_plane.Config{
		BaseURL:   server.URL,
		APIKey:    "stress-token",
		Workspace: "test-workspace",
		Timeout:   3 * time.Second,
	}
	client := tracker_plane.NewClient(cfg)

	const numWorkers = 20
	const opsPerWorker = 20

	var wg sync.WaitGroup
	ctx := context.Background()

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				switch j % 4 {
				case 0:
					_, _ = client.FetchIssues(ctx, "proj-1", "OMNIFY", 5)
				case 1:
					_, _ = client.FetchProjects(ctx)
				case 2:
					_, _ = client.FetchStates(ctx, "proj-1")
				case 3:
					_, _ = client.CreateIssueRemote(ctx, "proj-1", issue.CreateIssueInput{
						ProjectKey: "OMNIFY",
						Title:      fmt.Sprintf("Stress task %d-%d", workerID, j),
					}, "st-1")
				}
			}
		}(i)
	}

	wg.Wait()
	t.Logf("Total peticiones concurrentes completadas: %d", atomic.LoadInt64(&requestCount))
}

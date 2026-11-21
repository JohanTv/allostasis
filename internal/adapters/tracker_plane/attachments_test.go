package tracker_plane_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JohanTv/allostasis/internal/adapters/tracker_plane"
	"github.com/JohanTv/allostasis/internal/domain/issue"
)

const attBase = "/api/v1/workspaces/ws/projects/p1/issues/i1/issue-attachments/"

// fakePlane simula Plane CE y su almacenamiento firmado (S3/MinIO) y registra lo que recibe.
type fakePlane struct {
	mu sync.Mutex

	registerStatus int    // por defecto 200
	registerBody   string // si no está vacío, reemplaza la respuesta de registro
	storageStatus  int    // por defecto 204
	patchStatus    int    // por defecto 204
	deleteStatus   int    // por defecto 204
	relativeUpload bool

	registered    map[string]any
	storageFields []string // nombres de las partes, en orden de llegada
	storageFile   string
	storageAPIKey string // x-api-key recibido por el almacenamiento (debe quedar vacío)
	confirmed     bool
	deleted       []string
	storageCalls  int
}

func newFakePlane(t *testing.T, f *fakePlane) (*tracker_plane.Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		status := func(v, def int) int {
			if v == 0 {
				return def
			}
			return v
		}
		switch {
		case r.URL.Path == "/bucket/upload": // almacenamiento firmado
			f.storageCalls++
			f.storageAPIKey = r.Header.Get("x-api-key")
			_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				part, err := mr.NextPart()
				if err != nil {
					break
				}
				f.storageFields = append(f.storageFields, part.FormName())
				if part.FormName() == "file" {
					b, _ := io.ReadAll(part)
					f.storageFile = string(b)
				}
			}
			w.WriteHeader(status(f.storageStatus, http.StatusNoContent))
		case r.URL.Path == attBase && r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&f.registered)
			if f.registerStatus != 0 && f.registerStatus != http.StatusOK {
				w.WriteHeader(f.registerStatus)
				_, _ = w.Write([]byte(f.registerBody))
				return
			}
			uploadURL := "http://" + r.Host + "/bucket/upload"
			if f.relativeUpload {
				uploadURL = "/bucket/upload"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"asset_id":    "asset-1",
				"asset_url":   "http://files/asset-1",
				"upload_data": map[string]any{"url": uploadURL, "fields": map[string]string{"policy": "pol", "key": "k"}},
			})
		case r.URL.Path == attBase+"asset-1/" && r.Method == http.MethodPatch:
			var body map[string]bool
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.confirmed = body["is_uploaded"]
			w.WriteHeader(status(f.patchStatus, http.StatusNoContent))
		case strings.HasPrefix(r.URL.Path, attBase) && r.Method == http.MethodDelete:
			f.deleted = append(f.deleted, strings.TrimPrefix(r.URL.Path, attBase))
			w.WriteHeader(status(f.deleteStatus, http.StatusNoContent))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return tracker_plane.NewClient(tracker_plane.Config{BaseURL: srv.URL, APIKey: "secret-key", Workspace: "ws", Timeout: 5 * time.Second}), srv
}

func upload(content string) issue.AttachmentUpload {
	return issue.AttachmentUpload{Name: "nota.txt", ContentType: "text/plain", Size: int64(len(content)), Content: strings.NewReader(content)}
}

func TestUploadAttachment_ThreeStepProtocol(t *testing.T) {
	f := &fakePlane{}
	client, _ := newFakePlane(t, f)

	att, err := client.UploadAttachmentRemote(context.Background(), "p1", "i1", upload("hola!"))
	if err != nil {
		t.Fatalf("UploadAttachmentRemote falló: %v", err)
	}
	if att.ID != "asset-1" || att.Name != "nota.txt" || att.SizeBytes != 5 || att.AssetURL != "http://files/asset-1" {
		t.Errorf("adjunto inesperado: %+v", att)
	}
	if f.registered["name"] != "nota.txt" || f.registered["type"] != "text/plain" || f.registered["size"] != float64(5) {
		t.Errorf("registro enviado a Plane = %v", f.registered)
	}
	if got := strings.Join(f.storageFields, ","); got != "key,policy,file" {
		t.Errorf("partes del multipart = %q; los campos firmados van primero y el archivo al final", got)
	}
	if f.storageFile != "hola!" {
		t.Errorf("contenido subido = %q", f.storageFile)
	}
	if f.storageAPIKey != "" {
		t.Errorf("el x-api-key de Plane NO debe enviarse al almacenamiento firmado (se envió %q)", f.storageAPIKey)
	}
	if !f.confirmed {
		t.Error("falta confirmar con is_uploaded=true")
	}
	if len(f.deleted) != 0 {
		t.Errorf("un éxito no debe eliminar nada, eliminó %v", f.deleted)
	}
}

func TestUploadAttachment_RelativeUploadURLResolvesAgainstBase(t *testing.T) {
	f := &fakePlane{relativeUpload: true}
	client, _ := newFakePlane(t, f)
	if _, err := client.UploadAttachmentRemote(context.Background(), "p1", "i1", upload("x")); err != nil {
		t.Fatalf("una URL de subida relativa debe resolverse contra la base de Plane: %v", err)
	}
	if f.storageCalls != 1 {
		t.Errorf("llamadas al almacenamiento = %d", f.storageCalls)
	}
}

func TestUploadAttachment_FailuresRollBackThePendingAsset(t *testing.T) {
	cases := []struct {
		name     string
		fake     *fakePlane
		wantKind issue.RemoteErrorKind
	}{
		{"almacenamiento 503", &fakePlane{storageStatus: 503}, issue.RemoteErrTransient},
		{"almacenamiento 403", &fakePlane{storageStatus: 403}, issue.RemoteErrUnknown},
		{"confirmación 500", &fakePlane{patchStatus: 500}, issue.RemoteErrTransient},
		{"confirmación 400", &fakePlane{patchStatus: 400}, issue.RemoteErrRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.fake
			client, _ := newFakePlane(t, f)
			_, err := client.UploadAttachmentRemote(context.Background(), "p1", "i1", upload("data"))
			if err == nil {
				t.Fatal("debe fallar")
			}
			if got := issue.ClassifyRemoteError(err); got != tc.wantKind {
				t.Errorf("clase = %s, esperado %s (%v)", got, tc.wantKind, err)
			}
			if len(f.deleted) != 1 || f.deleted[0] != "asset-1/" {
				t.Errorf("debe eliminar el adjunto pendiente asset-1, eliminó %v", f.deleted)
			}
		})
	}
}

func TestUploadAttachment_RollbackFailureIsReported(t *testing.T) {
	f := &fakePlane{storageStatus: 503, deleteStatus: 500}
	client, _ := newFakePlane(t, f)
	_, err := client.UploadAttachmentRemote(context.Background(), "p1", "i1", upload("data"))
	if err == nil || !strings.Contains(err.Error(), "no se pudo eliminar el adjunto pendiente asset-1") {
		t.Fatalf("si la limpieza falla, el error debe decirlo para que un huérfano no quede oculto: %v", err)
	}
	if issue.ClassifyRemoteError(err) != issue.RemoteErrTransient {
		t.Errorf("la causa original (503) debe seguir clasificada: %v", err)
	}
}

func TestUploadAttachment_UnsupportedTypeAndSizeMismatch(t *testing.T) {
	f := &fakePlane{registerStatus: 400, registerBody: `{"error":"Invalid file type"}`}
	client, _ := newFakePlane(t, f)
	_, err := client.UploadAttachmentRemote(context.Background(), "p1", "i1", upload("x"))
	var invalid *issue.InvalidAttachmentError
	if !errors.As(err, &invalid) || invalid.Hint == "" {
		t.Fatalf("esperado *InvalidAttachmentError con pista, obtuvo %v", err)
	}
	if f.storageCalls != 0 {
		t.Error("no debe subirse nada si Plane rechazó el tipo")
	}

	bad := issue.AttachmentUpload{Name: "a.txt", ContentType: "text/plain", Size: 10, Content: strings.NewReader("corto")}
	f2 := &fakePlane{}
	client2, _ := newFakePlane(t, f2)
	if _, err := client2.UploadAttachmentRemote(context.Background(), "p1", "i1", bad); err == nil || f2.registered != nil {
		t.Errorf("un archivo que cambió de tamaño debe fallar antes de contactar a Plane (err=%v, registered=%v)", err, f2.registered)
	}
}

func TestUploadAttachment_CancellationIsNotTransient(t *testing.T) {
	f := &fakePlane{}
	client, _ := newFakePlane(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.UploadAttachmentRemote(ctx, "p1", "i1", upload("x"))
	if err == nil || issue.ClassifyRemoteError(err) == issue.RemoteErrTransient {
		t.Errorf("Ctrl+C no es un fallo reintentable: %v", err)
	}
}

func TestListAttachmentsRemote_ShapesAndCompleteness(t *testing.T) {
	cases := []struct {
		name         string
		body         string
		wantLen      int
		wantComplete bool
	}{
		{"arreglo", `[{"id":"a1","attributes":{"name":"x.txt","size":2048},"asset":"http://f/a1","created_at":"2026-11-18T10:00:00Z"}]`, 1, true},
		{"results", `{"results":[{"id":"a1","attributes":{"name":"x.txt","size":10}},{"id":"a2","size":7}]}`, 2, true},
		{"más páginas", `{"results":[{"id":"a1","attributes":{"name":"x.txt"}}],"next_page_results":true}`, 1, false},
		{"vacío", `[]`, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("x-api-key") != "secret-key" || r.URL.Path != attBase {
					http.NotFound(w, r)
					return
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			client := tracker_plane.NewClient(tracker_plane.Config{BaseURL: srv.URL, APIKey: "secret-key", Workspace: "ws", Timeout: 2 * time.Second})
			list, complete, err := client.ListAttachmentsRemote(context.Background(), "p1", "i1")
			if err != nil {
				t.Fatalf("falló: %v", err)
			}
			if len(list) != tc.wantLen || complete != tc.wantComplete {
				t.Errorf("len=%d complete=%v, esperado %d/%v", len(list), complete, tc.wantLen, tc.wantComplete)
			}
			if tc.name == "arreglo" && (list[0].Name != "x.txt" || list[0].SizeBytes != 2048 || list[0].AssetURL != "http://f/a1" || list[0].CreatedAt.IsZero()) {
				t.Errorf("campos mal leídos: %+v", list[0])
			}
		})
	}
}

func TestDeleteAttachmentRemote(t *testing.T) {
	for status, want := range map[int]issue.RemoteErrorKind{
		http.StatusNoContent: -1, http.StatusNotFound: issue.RemoteErrRejected,
		http.StatusForbidden: issue.RemoteErrAuth, http.StatusBadGateway: issue.RemoteErrTransient,
	} {
		f := &fakePlane{deleteStatus: status}
		client, _ := newFakePlane(t, f)
		err := client.DeleteAttachmentRemote(context.Background(), "p1", "i1", "asset-1")
		if want == -1 {
			if err != nil {
				t.Errorf("HTTP %d debe ser éxito: %v", status, err)
			}
			continue
		}
		if got := issue.ClassifyRemoteError(err); got != want {
			t.Errorf("HTTP %d: clase = %s, esperado %s", status, got, want)
		}
	}
}

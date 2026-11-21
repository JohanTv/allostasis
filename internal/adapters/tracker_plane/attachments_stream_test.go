package tracker_plane_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/allostasis/alo/internal/adapters/tracker_plane"
	"github.com/allostasis/alo/internal/domain/issue"
)

type repeatByteReader byte

func (b repeatByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(b)
	}
	return len(p), nil
}

func TestUploadAttachmentRemote_HeapAllocation_IsConstant(t *testing.T) {
	const largeSize = 50 * 1024 * 1024 // 50 MB (límite máximo admitido por Plane CE)

	var bytesReceived int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/bucket/upload":
			// El endpoint de S3/MinIO firmado verifica Content-Length
			if r.ContentLength <= 0 {
				t.Errorf("S3/MinIO requiere ContentLength > 0, obtenido %d", r.ContentLength)
				w.WriteHeader(http.StatusLengthRequired)
				return
			}
			n, err := io.Copy(io.Discard, r.Body)
			if err != nil {
				t.Errorf("error drenando body en bucket: %v", err)
			}
			bytesReceived = n
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost: // Registro de adjunto en Plane
			_ = json.NewEncoder(w).Encode(map[string]any{
				"asset_id":  "asset-stream-1",
				"asset_url": "http://files/asset-stream-1",
				"upload_data": map[string]any{
					"url":    "http://" + r.Host + "/bucket/upload",
					"fields": map[string]string{"policy": "pol", "key": "k"},
				},
			})
		case r.Method == http.MethodPatch: // Confirmación is_uploaded
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := tracker_plane.NewClient(tracker_plane.Config{
		BaseURL:   srv.URL,
		APIKey:    "key",
		Workspace: "ws",
		Timeout:   10 * time.Second,
	})

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// Stream sintético infinito con LimitReader: asigna 0 bytes de heap para el payload de 50 MB
	stream := io.LimitReader(repeatByteReader('Z'), largeSize)
	att, err := client.UploadAttachmentRemote(context.Background(), "p1", "i1", issue.AttachmentUpload{
		Name:        "large_payload.bin",
		ContentType: "application/octet-stream",
		Size:        largeSize,
		Content:     stream,
	})
	if err != nil {
		t.Fatalf("UploadAttachmentRemote falló: %v", err)
	}

	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	deltaAlloc := m2.TotalAlloc - m1.TotalAlloc

	if att.SizeBytes != largeSize {
		t.Errorf("tamaño registrado = %d, esperado %d", att.SizeBytes, largeSize)
	}
	if bytesReceived < largeSize {
		t.Errorf("servidor recibió %d bytes, esperado >= %d", bytesReceived, largeSize)
	}

	// El heap asignado acumulado durante toda la transferencia de 50 MB DEBE ser < 1 MB (O(1))
	const maxAllowedHeapAlloc = 1024 * 1024 // 1 MB
	if deltaAlloc > maxAllowedHeapAlloc {
		t.Fatalf("Asignación de heap excesiva: se asignaron %.2f KB (supera límite estricto de 1 MB)", float64(deltaAlloc)/1024)
	}
}

func TestUploadAttachmentRemote_StreamTruncatedInFlightFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/bucket/upload":
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"asset_id":  "asset-trunc-1",
				"asset_url": "http://files/asset-trunc-1",
				"upload_data": map[string]any{
					"url":    "http://" + r.Host + "/bucket/upload",
					"fields": map[string]string{"key": "k"},
				},
			})
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	client := tracker_plane.NewClient(tracker_plane.Config{
		BaseURL:   srv.URL,
		APIKey:    "key",
		Workspace: "ws",
		Timeout:   5 * time.Second,
	})

	// El lector solo entrega 5 bytes, pero se prometieron 100
	truncatedStream := io.LimitReader(repeatByteReader('A'), 5)
	_, err := client.UploadAttachmentRemote(context.Background(), "p1", "i1", issue.AttachmentUpload{
		Name:        "trunc.bin",
		ContentType: "application/octet-stream",
		Size:        100,
		Content:     truncatedStream,
	})
	if err == nil || !strings.Contains(err.Error(), "cambió mientras se leía") {
		t.Fatalf("se esperaba error de stream truncado en vuelo, obtenido: %v", err)
	}
}

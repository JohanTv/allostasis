package tracker_plane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/JohanTv/allostasis/internal/domain/issue"
)

const (
	// uploadTimeout acota la subida al almacenamiento firmado (hasta 50 MB), más holgada que una llamada a la API.
	uploadTimeout = 2 * time.Minute
	// rollbackTimeout acota la limpieza de un adjunto pendiente; usa su propio contexto porque el original
	// puede estar ya cancelado cuando la subida falla.
	rollbackTimeout = 15 * time.Second
)

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string {
	return quoteEscaper.Replace(s)
}

func inspectCurrentSize(r io.Reader) (int64, bool) {
	if st, ok := r.(interface{ Stat() (os.FileInfo, error) }); ok {
		if info, err := st.Stat(); err == nil {
			return info.Size(), true
		}
	}
	if sz, ok := r.(interface{ Size() int64 }); ok {
		return sz.Size(), true
	}
	if ln, ok := r.(interface{ Len() int }); ok {
		return int64(ln.Len()), true
	}
	return 0, false
}

// exactSizeReader audita el flujo de bytes transmitidos en streaming para verificar
// que coincida exactamente con el tamaño acordado sin bufferizar en memoria.
type exactSizeReader struct {
	r     io.Reader
	name  string
	total int64
	read  int64
}

func (e *exactSizeReader) Read(p []byte) (int, error) {
	if e.read >= e.total {
		var extra [1]byte
		n, err := e.r.Read(extra[:])
		if n > 0 {
			return 0, fmt.Errorf("'%s' cambió mientras se leía (superó los %d bytes esperados)", e.name, e.total)
		}
		if err != nil && err != io.EOF {
			return 0, err
		}
		return 0, io.EOF
	}

	remaining := e.total - e.read
	toRead := p
	if int64(len(toRead)) > remaining {
		toRead = toRead[:remaining]
	}

	n, err := e.r.Read(toRead)
	e.read += int64(n)
	if err == io.EOF && e.read < e.total {
		return n, fmt.Errorf("'%s' cambió mientras se leía (se esperaban %d bytes, se leyeron %d)", e.name, e.total, e.read)
	}
	return n, err
}

func (c *Client) attachmentsURL(projectID, issueID string) string {
	return fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issues/%s/issue-attachments/", c.cfg.BaseURL, c.cfg.Workspace, projectID, issueID)
}

// UploadAttachmentRemote sube un archivo a una tarea en tres pasos en streaming continuo O(1) de memoria:
// 1) registra el adjunto y recibe una subida firmada, 2) transmite el stream al almacenamiento firmado y
// 3) confirma con is_uploaded. Si el paso 2 o 3 falla, elimina el adjunto pendiente para no dejarlo huérfano.
func (c *Client) UploadAttachmentRemote(ctx context.Context, projectID, issueID string, up issue.AttachmentUpload) (*issue.Attachment, error) {
	if c == nil || c.cfg.APIKey == "" {
		return nil, fmt.Errorf("PLANE_API_KEY no configurada")
	}
	if up.Size > issue.MaxAttachmentBytes {
		return nil, &issue.InvalidAttachmentError{
			Path:   up.Name,
			Reason: fmt.Sprintf("supera el máximo de %d MB", issue.MaxAttachmentBytes/(1024*1024)),
		}
	}
	// Pre-inspección en O(1) de tamaño si la fuente lo expone (os.File, bytes.Reader, strings.Reader)
	if curr, ok := inspectCurrentSize(up.Content); ok && curr != up.Size {
		return nil, fmt.Errorf("'%s' cambió mientras se leía (se esperaban %d bytes, se leyeron %d)", up.Name, up.Size, curr)
	}

	// 1. Registrar el adjunto y obtener la subida firmada.
	meta, _ := json.Marshal(map[string]interface{}{"name": up.Name, "type": up.ContentType, "size": up.Size})
	respBytes, status, err := c.doJSON(ctx, http.MethodPost, c.attachmentsURL(projectID, issueID), meta)
	if err != nil {
		return nil, transportError("registrar adjunto", err)
	}
	if status != http.StatusOK && status != http.StatusCreated {
		if status == http.StatusBadRequest && isUnsupportedFileType(respBytes) {
			return nil, &issue.InvalidAttachmentError{
				Path:   up.Name,
				Reason: "Plane rechazó el tipo de archivo",
				Hint:   "Convierte o renombra el archivo a .txt o .md para subirlo como adjunto",
			}
		}
		return nil, mutationError(status, "registrar adjunto", respBytes)
	}
	var created struct {
		AssetID    string `json:"asset_id"`
		AssetURL   string `json:"asset_url"`
		UploadData struct {
			URL    string            `json:"url"`
			Fields map[string]string `json:"fields"`
		} `json:"upload_data"`
	}
	if err := json.Unmarshal(respBytes, &created); err != nil || created.AssetID == "" || created.UploadData.URL == "" {
		return nil, fmt.Errorf("respuesta inesperada de Plane al registrar el adjunto (sin asset_id o upload_data): %s", truncate(respBytes))
	}

	// 2 y 3, con streaming O(1) hacia almacenamiento firmado y limpieza del adjunto pendiente si algo falla.
	if err := c.sendToStorage(ctx, created.UploadData.URL, created.UploadData.Fields, up); err != nil {
		return nil, c.rollbackAttachment(projectID, issueID, created.AssetID, err)
	}
	confirm, _ := json.Marshal(map[string]bool{"is_uploaded": true})
	body, status, err := c.doJSON(ctx, http.MethodPatch, c.attachmentsURL(projectID, issueID)+created.AssetID+"/", confirm)
	if err != nil {
		return nil, c.rollbackAttachment(projectID, issueID, created.AssetID, transportError("confirmar adjunto", err))
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return nil, c.rollbackAttachment(projectID, issueID, created.AssetID, mutationError(status, "confirmar adjunto", body))
	}

	return &issue.Attachment{
		ID:        created.AssetID,
		IssueID:   issueID,
		Name:      up.Name,
		SizeBytes: up.Size,
		AssetURL:  created.AssetURL,
		CreatedAt: time.Now(),
	}, nil
}

// sendToStorage transmite el archivo en streaming continuo (O(1) memoria) hacia el almacenamiento firmado.
// S3 y MinIO exigen Content-Length conocido de antemano (rechazan chunked encoding) y requieren que los campos
// firmados precedan obligatoriamente al part del archivo.
func (c *Client) sendToStorage(ctx context.Context, rawURL string, fields map[string]string, up issue.AttachmentUpload) error {
	target, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("URL de subida inválida: %w", err)
	}
	if base, err := url.Parse(c.cfg.BaseURL); err == nil {
		target = base.ResolveReference(target)
	}

	// 1. Construir el prefijo multipart con los campos ordenados y el header del archivo (~1 KB)
	var prefix bytes.Buffer
	w := multipart.NewWriter(&prefix)

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := w.WriteField(k, fields[k]); err != nil {
			return err
		}
	}

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuotes(up.Name)))
	contentType := up.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	h.Set("Content-Type", contentType)
	if _, err := w.CreatePart(h); err != nil {
		return err
	}

	// 2. Sufijo exacto de cierre multipart
	suffix := []byte(fmt.Sprintf("\r\n--%s--\r\n", w.Boundary()))

	// 3. Cálculo matemático exacto de Content-Length
	totalContentLength := int64(prefix.Len()) + up.Size + int64(len(suffix))

	// 4. Encadenamiento MultiReader sin tocar RAM para el cuerpo del archivo
	checkedContent := &exactSizeReader{
		r:     up.Content,
		name:  up.Name,
		total: up.Size,
	}
	bodyStream := io.MultiReader(bytes.NewReader(prefix.Bytes()), checkedContent, bytes.NewReader(suffix))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bodyStream)
	if err != nil {
		return err
	}
	req.ContentLength = totalContentLength
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := (&http.Client{Timeout: uploadTimeout}).Do(req)
	if err != nil {
		return transportError("subir archivo", err)
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if resp.StatusCode >= 500 {
			return &issue.TransientError{Operation: "subir archivo al almacenamiento", Status: resp.StatusCode, Detail: truncate(respBytes)}
		}
		return fmt.Errorf("el almacenamiento rechazó la subida (HTTP %d): %s", resp.StatusCode, truncate(respBytes))
	}
	return nil
}

// rollbackAttachment elimina un adjunto pendiente tras un fallo y devuelve el error original; si la
// limpieza también falla, lo dice, para que un huérfano nunca quede oculto.
func (c *Client) rollbackAttachment(projectID, issueID, assetID string, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
	defer cancel()
	if err := c.DeleteAttachmentRemote(ctx, projectID, issueID, assetID); err != nil {
		return fmt.Errorf("%w; además no se pudo eliminar el adjunto pendiente %s: %v", cause, assetID, err)
	}
	return cause
}

// ListAttachmentsRemote lista los adjuntos de una tarea. El booleano indica si la lista está completa
// (Plane puede anunciar más páginas).
func (c *Client) ListAttachmentsRemote(ctx context.Context, projectID, issueID string) ([]issue.Attachment, bool, error) {
	if c == nil || c.cfg.APIKey == "" {
		return nil, false, fmt.Errorf("PLANE_API_KEY no configurada")
	}
	body, status, err := c.doJSON(ctx, http.MethodGet, c.attachmentsURL(projectID, issueID), nil)
	if err != nil {
		return nil, false, transportError("listar adjuntos", err)
	}
	if status != http.StatusOK {
		return nil, false, mutationError(status, "listar adjuntos", body)
	}

	var raw []map[string]interface{}
	complete := true
	var wrapper struct {
		Results         []map[string]interface{} `json:"results"`
		NextPageResults bool                     `json:"next_page_results"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		if err := json.Unmarshal(body, &wrapper); err != nil {
			return nil, false, fmt.Errorf("respuesta inesperada de Plane al listar adjuntos: %s", truncate(body))
		}
		raw, complete = wrapper.Results, !wrapper.NextPageResults
	}

	list := make([]issue.Attachment, 0, len(raw))
	for _, item := range raw {
		id, _ := item["id"].(string)
		if id == "" {
			continue
		}
		attrs, _ := item["attributes"].(map[string]interface{})
		name, _ := attrs["name"].(string)
		size, _ := attrs["size"].(float64)
		if size == 0 {
			size, _ = item["size"].(float64)
		}
		assetURL, _ := item["asset"].(string)
		createdAt, _ := time.Parse(time.RFC3339, fmt.Sprint(item["created_at"]))
		list = append(list, issue.Attachment{ID: id, IssueID: issueID, Name: name, SizeBytes: int64(size), AssetURL: assetURL, CreatedAt: createdAt})
	}
	return list, complete, nil
}

// DeleteAttachmentRemote elimina un adjunto de una tarea en Plane CE.
func (c *Client) DeleteAttachmentRemote(ctx context.Context, projectID, issueID, attachmentID string) error {
	if c == nil || c.cfg.APIKey == "" {
		return fmt.Errorf("PLANE_API_KEY no configurada")
	}
	body, status, err := c.doJSON(ctx, http.MethodDelete, c.attachmentsURL(projectID, issueID)+attachmentID+"/", nil)
	if err != nil {
		return transportError("eliminar adjunto", err)
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return mutationError(status, "eliminar adjunto", body)
	}
	return nil
}

// doJSON ejecuta una petición autenticada a la API de Plane y devuelve cuerpo y status.
func (c *Client) doJSON(ctx context.Context, method, target string, payload []byte) ([]byte, int, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func isUnsupportedFileType(body []byte) bool {
	lower := strings.ToLower(string(body))
	for _, marker := range []string{"invalid file type", "file type not allowed", "unsupported file type"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func truncate(b []byte) string {
	const max = 300
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}

package tracker_plane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/allostasis/alo/internal/domain/issue"
)

const (
	// uploadTimeout acota la subida al almacenamiento firmado (hasta 50 MB), más holgada que una llamada a la API.
	uploadTimeout = 2 * time.Minute
	// rollbackTimeout acota la limpieza de un adjunto pendiente; usa su propio contexto porque el original
	// puede estar ya cancelado cuando la subida falla.
	rollbackTimeout = 15 * time.Second
)

func (c *Client) attachmentsURL(projectID, issueID string) string {
	return fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issues/%s/issue-attachments/", c.cfg.BaseURL, c.cfg.Workspace, projectID, issueID)
}

// UploadAttachmentRemote sube un archivo a una tarea en tres pasos (el protocolo de adjuntos de Plane CE):
// 1) registra el adjunto y recibe una subida firmada, 2) envía el archivo al almacenamiento firmado y
// 3) confirma con is_uploaded. Si el paso 2 o 3 falla, elimina el adjunto pendiente para no dejarlo huérfano.
func (c *Client) UploadAttachmentRemote(ctx context.Context, projectID, issueID string, up issue.AttachmentUpload) (*issue.Attachment, error) {
	if c == nil || c.cfg.APIKey == "" {
		return nil, fmt.Errorf("PLANE_API_KEY no configurada")
	}
	data, err := io.ReadAll(io.LimitReader(up.Content, issue.MaxAttachmentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("error leyendo '%s': %w", up.Name, err)
	}
	if int64(len(data)) != up.Size {
		return nil, fmt.Errorf("'%s' cambió mientras se leía (se esperaban %d bytes, se leyeron %d)", up.Name, up.Size, len(data))
	}

	// 1. Registrar el adjunto y obtener la subida firmada.
	meta, _ := json.Marshal(map[string]interface{}{"name": up.Name, "type": up.ContentType, "size": len(data)})
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

	// 2 y 3, con limpieza del adjunto pendiente si algo falla.
	if err := c.sendToStorage(ctx, created.UploadData.URL, created.UploadData.Fields, up, data); err != nil {
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
		SizeBytes: int64(len(data)),
		AssetURL:  created.AssetURL,
		CreatedAt: time.Now(),
	}, nil
}

// sendToStorage envía el archivo al almacenamiento firmado. Es una URL de terceros (S3/MinIO): NUNCA lleva
// el x-api-key de Plane. Los campos firmados van primero y el archivo al final, como exige el POST de S3.
func (c *Client) sendToStorage(ctx context.Context, rawURL string, fields map[string]string, up issue.AttachmentUpload, data []byte) error {
	target, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("URL de subida inválida: %w", err)
	}
	if base, err := url.Parse(c.cfg.BaseURL); err == nil {
		target = base.ResolveReference(target)
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
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
	part, err := w.CreateFormFile("file", up.Name)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), &body)
	if err != nil {
		return err
	}
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

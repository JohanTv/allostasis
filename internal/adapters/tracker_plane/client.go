package tracker_plane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/allostasis/alo/internal/domain/issue"
)

// Config almacena las credenciales y URLs de conexión a Plane CE.
type Config struct {
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"api_key"`
	Workspace string `json:"workspace"`
	Timeout   time.Duration
}

// UserInfo representa los metadatos de usuario devueltos por Plane CE (alias a issue.UserInfo).
type UserInfo = issue.UserInfo

// SyncReport resume las operaciones ejecutadas durante el ciclo de sincronización (alias a issue.SyncReport).
type SyncReport = issue.SyncReport

// LoadConfigFromEnv carga la configuración desde las variables de entorno canónicas.
func LoadConfigFromEnv() Config {
	baseURL := os.Getenv("PLANE_BASE_URL")
	if baseURL == "" {
		baseURL = os.Getenv("PLANE_URL")
	}
	if baseURL == "" {
		baseURL = "http://localhost:8000"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	apiKey := os.Getenv("PLANE_API_KEY")

	workspace := os.Getenv("PLANE_WORKSPACE")
	if workspace == "" {
		workspace = "teamprogra"
	}

	return Config{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		Workspace: workspace,
		Timeout:   15 * time.Second,
	}
}

// Client gestiona el transporte HTTP puro con la API REST de Plane CE (sin acoplamiento a bases de datos).
type Client struct {
	cfg        Config
	httpClient *http.Client
}

// NewClient inicializa el cliente HTTP.
func NewClient(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// Whoami valida la conectividad y las credenciales con Plane CE retornando datos del usuario actual.
func (c *Client) Whoami(ctx context.Context) (*UserInfo, error) {
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf("PLANE_API_KEY no configurada")
	}
	url := fmt.Sprintf("%s/api/v1/users/me/", c.cfg.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error de red al conectar con Plane (%s): %w", c.cfg.BaseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("autenticación rechazada (HTTP 401): verifica PLANE_API_KEY")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Plane retornó HTTP %d: %s", resp.StatusCode, string(body))
	}

	var user UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("error decodificando respuesta de usuario: %w", err)
	}
	return &user, nil
}

// FetchProjects descarga el catálogo de proyectos del workspace desde Plane CE.
func (c *Client) FetchProjects(ctx context.Context) ([]issue.ProjectRecord, error) {
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf("PLANE_API_KEY no configurada")
	}
	url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/", c.cfg.BaseURL, c.cfg.Workspace)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error de red al sincronizar proyectos: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Plane retornó HTTP %d al listar proyectos: %s", resp.StatusCode, string(body))
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	var rawProjects []map[string]interface{}

	var wrapper struct {
		Results []map[string]interface{} `json:"results"`
	}
	if err := json.Unmarshal(bodyBytes, &wrapper); err == nil && len(wrapper.Results) > 0 {
		rawProjects = wrapper.Results
	} else {
		_ = json.Unmarshal(bodyBytes, &rawProjects)
	}

	var records []issue.ProjectRecord
	for _, p := range rawProjects {
		id, _ := p["id"].(string)
		identifier, _ := p["identifier"].(string)
		name, _ := p["name"].(string)
		if id == "" || identifier == "" {
			continue
		}
		rec := issue.ProjectRecord{
			Identifier: strings.ToUpper(identifier),
			ProjectID:  id,
			Name:       name,
			Workspace:  c.cfg.Workspace,
		}
		records = append(records, rec)
	}
	return records, nil
}

// FetchStates descarga los estados de un proyecto específico desde Plane CE.
func (c *Client) FetchStates(ctx context.Context, projectID string) ([]issue.StateRecord, error) {
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf("PLANE_API_KEY no configurada")
	}
	url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/states/", c.cfg.BaseURL, c.cfg.Workspace, projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error de red al sincronizar estados: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Plane retornó HTTP %d al listar estados: %s", resp.StatusCode, string(body))
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	var rawStates []map[string]interface{}
	var wrapper struct {
		Results []map[string]interface{} `json:"results"`
	}
	if err := json.Unmarshal(bodyBytes, &wrapper); err == nil && len(wrapper.Results) > 0 {
		rawStates = wrapper.Results
	} else {
		_ = json.Unmarshal(bodyBytes, &rawStates)
	}

	var records []issue.StateRecord
	for _, s := range rawStates {
		id, _ := s["id"].(string)
		name, _ := s["name"].(string)
		slug, _ := s["slug"].(string)
		group, _ := s["group"].(string)
		if id == "" || name == "" {
			continue
		}
		rec := issue.StateRecord{
			ID:        id,
			ProjectID: projectID,
			Name:      name,
			Slug:      slug,
			GroupName: group,
		}
		records = append(records, rec)
	}
	return records, nil
}

// FetchLabels descarga las etiquetas de un proyecto desde Plane CE.
func (c *Client) FetchLabels(ctx context.Context, projectID string) ([]issue.Label, error) {
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf("PLANE_API_KEY no configurada")
	}

	url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issue-labels/", c.cfg.BaseURL, c.cfg.Workspace, projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error al obtener etiquetas de Plane (HTTP %d)", resp.StatusCode)
	}

	var res []map[string]interface{}
	respBytes, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(respBytes, &res)

	var labels []issue.Label
	for _, l := range res {
		id, _ := l["id"].(string)
		name, _ := l["name"].(string)
		color, _ := l["color"].(string)
		if id != "" && name != "" {
			labels = append(labels, issue.Label{
				ID:        id,
				ProjectID: projectID,
				Name:      name,
				Color:     color,
			})
		}
	}
	return labels, nil
}

// FetchIssues descarga las tareas actualizadas de un proyecto desde Plane CE.
func (c *Client) FetchIssues(ctx context.Context, projectID, projectKey string, limit int) ([]issue.IssueSnapshot, error) {
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf("PLANE_API_KEY no configurada")
	}
	if limit <= 0 {
		limit = 100
	}
	url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issues/?order_by=-updated_at&per_page=%d", c.cfg.BaseURL, c.cfg.Workspace, projectID, limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error de red al descargar tareas de Plane: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Plane retornó HTTP %d al listar tareas: %s", resp.StatusCode, string(body))
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	var rawIssues []map[string]interface{}
	var wrapper struct {
		Results []map[string]interface{} `json:"results"`
	}
	if err := json.Unmarshal(bodyBytes, &wrapper); err == nil && len(wrapper.Results) > 0 {
		rawIssues = wrapper.Results
	} else {
		_ = json.Unmarshal(bodyBytes, &rawIssues)
	}

	var snapshots []issue.IssueSnapshot
	for _, it := range rawIssues {
		id, _ := it["id"].(string)
		name, _ := it["name"].(string)
		seq, _ := it["sequence_id"].(float64)
		priority, _ := it["priority"].(string)
		if priority == "" {
			priority = "none"
		}

		ticketKey := fmt.Sprintf("%s-%d", projectKey, int(seq))
		stateName := "Backlog"
		stateGroup := "backlog"

		if stDetail, ok := it["state_detail"].(map[string]interface{}); ok {
			if sn, ok := stDetail["name"].(string); ok && sn != "" {
				stateName = sn
			}
			if sg, ok := stDetail["group"].(string); ok && sg != "" {
				stateGroup = sg
			}
		}

		item := issue.WorkItem{
			ID:         id,
			Key:        ticketKey,
			ProjectID:  projectID,
			ProjectKey: projectKey,
			SequenceID: int(seq),
			Title:      name,
			StateName:  stateName,
			StateGroup: stateGroup,
			State:      issue.State(stateGroup),
			Priority:   priority,
		}

		if labelsArr, ok := it["labels"].([]interface{}); ok {
			var labelNames []string
			for _, l := range labelsArr {
				if lStr, ok := l.(string); ok {
					labelNames = append(labelNames, lStr)
				} else if lMap, ok := l.(map[string]interface{}); ok {
					if ln, ok := lMap["name"].(string); ok {
						labelNames = append(labelNames, ln)
					}
				}
			}
			item.Labels = labelNames
			item.LabelsCSV = strings.Join(labelNames, ",")
		}

		rawJSONBytes, _ := json.Marshal(it)
		snapshots = append(snapshots, issue.IssueSnapshot{
			Item:    item,
			RawJSON: string(rawJSONBytes),
		})
	}

	return snapshots, nil
}

// SendCommentRaw envía un payload JSON de comentario hacia Plane CE sin intermediación de base de datos.
func (c *Client) SendCommentRaw(ctx context.Context, targetWS, projectID, issueID, payloadJSON string) (string, error) {
	if c.cfg.APIKey == "" {
		return "", fmt.Errorf("PLANE_API_KEY no configurada")
	}
	if targetWS == "" {
		targetWS = c.cfg.Workspace
	}

	url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issues/%s/comments/", c.cfg.BaseURL, targetWS, projectID, issueID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString(payloadJSON))
	if err != nil {
		return "", err
	}

	req.Header.Set("x-api-key", c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("error de red al enviar a Plane: %w", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		var respPayload map[string]interface{}
		commentID := ""
		if json.Unmarshal(respBytes, &respPayload) == nil {
			if cid, ok := respPayload["id"].(string); ok {
				commentID = cid
			}
		}
		return commentID, nil
	} else if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("autenticación rechazada en Plane (HTTP 401): verifica PLANE_API_KEY")
	}

	return "", fmt.Errorf("Plane retornó HTTP %d: %s", resp.StatusCode, string(respBytes))
}

// CreateIssueRemote crea una tarea en Plane CE y retorna el modelo creado.
func (c *Client) CreateIssueRemote(ctx context.Context, projectID string, input issue.CreateIssueInput, stateID string) (*issue.WorkItem, error) {
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf("PLANE_API_KEY no configurada")
	}

	payloadMap := map[string]interface{}{
		"name":     input.Title,
		"priority": input.Priority,
	}
	if stateID != "" {
		payloadMap["state"] = stateID
	}
	if input.Description != "" {
		payloadMap["description_html"] = fmt.Sprintf("<p>%s</p>", input.Description)
	}

	payloadBytes, _ := json.Marshal(payloadMap)
	url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issues/", c.cfg.BaseURL, c.cfg.Workspace, projectID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error de red al crear tarea en Plane: %w", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("Plane retornó HTTP %d al crear tarea: %s", resp.StatusCode, string(respBytes))
	}

	var created map[string]interface{}
	if err := json.Unmarshal(respBytes, &created); err != nil {
		return nil, fmt.Errorf("error decodificando tarea creada: %w", err)
	}

	id, _ := created["id"].(string)
	seq, _ := created["sequence_id"].(float64)
	ticketKey := fmt.Sprintf("%s-%d", input.ProjectKey, int(seq))

	return &issue.WorkItem{
		ID:          id,
		Key:         ticketKey,
		ProjectID:   projectID,
		ProjectKey:  input.ProjectKey,
		SequenceID:  int(seq),
		Title:       input.Title,
		Description: input.Description,
		StateName:   input.StateName,
		Priority:    input.Priority,
	}, nil
}

// UpdateIssueRemote actualiza los campos de una tarea en Plane CE.
func (c *Client) UpdateIssueRemote(ctx context.Context, projectID, issueID string, input issue.UpdateIssueInput, stateID string) error {
	if c.cfg.APIKey == "" {
		return fmt.Errorf("PLANE_API_KEY no configurada")
	}

	payloadMap := make(map[string]interface{})
	if input.Title != nil {
		payloadMap["name"] = *input.Title
	}
	if input.Priority != nil {
		payloadMap["priority"] = *input.Priority
	}
	if stateID != "" {
		payloadMap["state"] = stateID
	}
	if input.Description != nil {
		payloadMap["description_html"] = fmt.Sprintf("<p>%s</p>", *input.Description)
	}

	payloadBytes, _ := json.Marshal(payloadMap)
	url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issues/%s/", c.cfg.BaseURL, c.cfg.Workspace, projectID, issueID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error de red al actualizar tarea en Plane: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Plane retornó HTTP %d al actualizar tarea: %s", resp.StatusCode, string(respBytes))
	}

	return nil
}

// DeleteIssueRemote elimina un work item en Plane CE.
func (c *Client) DeleteIssueRemote(ctx context.Context, projectID, issueID string) error {
	if c.cfg.APIKey == "" {
		return fmt.Errorf("PLANE_API_KEY no configurada")
	}

	url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issues/%s/", c.cfg.BaseURL, c.cfg.Workspace, projectID, issueID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error de red al eliminar tarea en Plane: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Plane retornó HTTP %d al eliminar tarea: %s", resp.StatusCode, string(respBytes))
	}

	return nil
}

// AddLinkRemote asocia un enlace a una tarea en Plane CE.
func (c *Client) AddLinkRemote(ctx context.Context, projectID, issueID, title, linkURL string) (*issue.Link, error) {
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf("PLANE_API_KEY no configurada")
	}

	payload := map[string]string{
		"title": title,
		"url":   linkURL,
	}
	payloadBytes, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issues/%s/links/", c.cfg.BaseURL, c.cfg.Workspace, projectID, issueID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error de red al agregar enlace en Plane: %w", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("Plane retornó HTTP %d al agregar enlace: %s", resp.StatusCode, string(respBytes))
	}

	var res map[string]interface{}
	_ = json.Unmarshal(respBytes, &res)
	id, _ := res["id"].(string)

	return &issue.Link{
		ID:        id,
		IssueID:   issueID,
		Title:     title,
		URL:       linkURL,
		CreatedAt: time.Now(),
	}, nil
}

// ListLinksRemote consulta los enlaces de una tarea en Plane CE.
func (c *Client) ListLinksRemote(ctx context.Context, projectID, issueID string) ([]issue.Link, error) {
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf("PLANE_API_KEY no configurada")
	}

	url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issues/%s/links/", c.cfg.BaseURL, c.cfg.Workspace, projectID, issueID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var res []map[string]interface{}
	respBytes, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(respBytes, &res)

	list := make([]issue.Link, 0)
	for _, item := range res {
		id, _ := item["id"].(string)
		title, _ := item["title"].(string)
		linkURL, _ := item["url"].(string)
		list = append(list, issue.Link{
			ID:        id,
			IssueID:   issueID,
			Title:     title,
			URL:       linkURL,
			CreatedAt: time.Now(),
		})
	}
	return list, nil
}

// DeleteLinkRemote remueve un enlace de una tarea en Plane CE.
func (c *Client) DeleteLinkRemote(ctx context.Context, projectID, issueID, linkID string) error {
	if c.cfg.APIKey == "" {
		return fmt.Errorf("PLANE_API_KEY no configurada")
	}

	url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issues/%s/links/%s/", c.cfg.BaseURL, c.cfg.Workspace, projectID, issueID, linkID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("error al eliminar enlace (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// EditCommentRemote actualiza el contenido de un comentario en Plane CE.
func (c *Client) EditCommentRemote(ctx context.Context, projectID, issueID, commentID, newBody string) error {
	if c.cfg.APIKey == "" {
		return fmt.Errorf("PLANE_API_KEY no configurada")
	}

	payload := map[string]string{
		"comment_html": fmt.Sprintf("<p>%s</p>", newBody),
	}
	payloadBytes, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issues/%s/comments/%s/", c.cfg.BaseURL, c.cfg.Workspace, projectID, issueID, commentID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("error al editar comentario (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// DeleteCommentRemote elimina un comentario en Plane CE.
func (c *Client) DeleteCommentRemote(ctx context.Context, projectID, issueID, commentID string) error {
	if c.cfg.APIKey == "" {
		return fmt.Errorf("PLANE_API_KEY no configurada")
	}

	url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issues/%s/comments/%s/", c.cfg.BaseURL, c.cfg.Workspace, projectID, issueID, commentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("error al eliminar comentario (HTTP %d)", resp.StatusCode)
	}
	return nil
}

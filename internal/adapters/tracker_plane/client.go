package tracker_plane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
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

	timeoutSec := 45
	if envTimeout := os.Getenv("PLANE_TIMEOUT"); envTimeout != "" {
		if t, err := strconv.Atoi(envTimeout); err == nil && t > 0 {
			timeoutSec = t
		}
	}

	return Config{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		Workspace: workspace,
		Timeout:   time.Duration(timeoutSec) * time.Second,
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

// parseSequenceID convierte de manera segura cualquier representación de sequence_id a entero.
func parseSequenceID(v interface{}) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case float32:
		return int(val)
	case int:
		return val
	case int64:
		return int(val)
	case int32:
		return int(val)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(val))
		return n
	case json.Number:
		n, _ := val.Int64()
		return int(n)
	default:
		return 0
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

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error leyendo cuerpo de respuesta de usuario: %w", err)
	}

	var user UserInfo
	if err := json.Unmarshal(bodyBytes, &user); err != nil {
		return nil, fmt.Errorf("error decodificando respuesta de usuario: %w", err)
	}
	if user.Username == "" && user.DisplayName != "" {
		user.Username = user.DisplayName
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

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error de lectura en flujo de proyectos: %w", err)
	}
	var rawProjects []map[string]interface{}

	var wrapper struct {
		Results []map[string]interface{} `json:"results"`
	}
	if err := json.Unmarshal(bodyBytes, &wrapper); err == nil {
		rawProjects = wrapper.Results
	} else if err := json.Unmarshal(bodyBytes, &rawProjects); err != nil {
		trimmed := bytes.TrimSpace(bodyBytes)
		if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("{}")) && !bytes.Equal(trimmed, []byte("null")) {
			return nil, fmt.Errorf("error decodificando proyectos de Plane (JSON corrupto): %w", err)
		}
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

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error de lectura en flujo de estados: %w", err)
	}
	var rawStates []map[string]interface{}
	var wrapper struct {
		Results []map[string]interface{} `json:"results"`
	}
	if err := json.Unmarshal(bodyBytes, &wrapper); err == nil {
		rawStates = wrapper.Results
	} else if err := json.Unmarshal(bodyBytes, &rawStates); err != nil {
		trimmed := bytes.TrimSpace(bodyBytes)
		if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("{}")) && !bytes.Equal(trimmed, []byte("null")) {
			return nil, fmt.Errorf("error decodificando estados de Plane (JSON corrupto): %w", err)
		}
	}

	var records []issue.StateRecord
	for _, s := range rawStates {
		if s == nil {
			continue
		}
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
	if records == nil {
		records = make([]issue.StateRecord, 0)
	}
	return records, nil
}

// FetchLabels descarga las etiquetas de un proyecto desde Plane CE.
func (c *Client) FetchLabels(ctx context.Context, projectID string) ([]issue.Label, error) {
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf("PLANE_API_KEY no configurada")
	}

	url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/labels/", c.cfg.BaseURL, c.cfg.Workspace, projectID)
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
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Plane retornó HTTP %d al listar etiquetas: %s", resp.StatusCode, string(body))
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error de lectura en flujo de etiquetas: %w", err)
	}
	var rawLabels []map[string]interface{}
	var wrapper struct {
		Results []map[string]interface{} `json:"results"`
	}
	if err := json.Unmarshal(respBytes, &wrapper); err == nil {
		rawLabels = wrapper.Results
	} else if err := json.Unmarshal(respBytes, &rawLabels); err != nil {
		trimmed := bytes.TrimSpace(respBytes)
		if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("{}")) && !bytes.Equal(trimmed, []byte("null")) {
			return nil, fmt.Errorf("error decodificando etiquetas de Plane (JSON corrupto): %w", err)
		}
	}

	var labels []issue.Label
	for _, l := range rawLabels {
		if l == nil {
			continue
		}
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
	if labels == nil {
		labels = make([]issue.Label, 0)
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

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error de lectura en flujo de tareas: %w", err)
	}
	var rawIssues []map[string]interface{}
	var wrapper struct {
		Results []map[string]interface{} `json:"results"`
	}
	if err := json.Unmarshal(bodyBytes, &wrapper); err == nil {
		rawIssues = wrapper.Results
	} else if err := json.Unmarshal(bodyBytes, &rawIssues); err != nil {
		trimmed := bytes.TrimSpace(bodyBytes)
		if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("{}")) && !bytes.Equal(trimmed, []byte("null")) {
			return nil, fmt.Errorf("error decodificando tareas de Plane (JSON corrupto): %w", err)
		}
	}

	stateMap := make(map[string]issue.StateRecord)
	if states, err := c.FetchStates(ctx, projectID); err == nil {
		for _, st := range states {
			stateMap[st.ID] = st
		}
	}

	labelMap := make(map[string]string)
	if labels, err := c.FetchLabels(ctx, projectID); err == nil {
		for _, l := range labels {
			labelMap[l.ID] = l.Name
		}
	}

	var snapshots []issue.IssueSnapshot
	for _, it := range rawIssues {
		if it == nil {
			continue
		}
		id, _ := it["id"].(string)
		name, _ := it["name"].(string)
		seq := parseSequenceID(it["sequence_id"])
		priority, _ := it["priority"].(string)
		if priority == "" {
			priority = "none"
		}

		ticketKey := fmt.Sprintf("%s-%d", projectKey, seq)
		if seq == 0 && id != "" {
			ticketKey = fmt.Sprintf("%s-%s", projectKey, id)
		}
		stateName := "Backlog"
		stateGroup := "backlog"

		if stDetail, ok := it["state_detail"].(map[string]interface{}); ok && stDetail != nil {
			if sn, ok := stDetail["name"].(string); ok && sn != "" {
				stateName = sn
			}
			if sg, ok := stDetail["group"].(string); ok && sg != "" {
				stateGroup = sg
			}
		} else if sID, ok := it["state"].(string); ok && sID != "" {
			if st, found := stateMap[sID]; found {
				stateName = st.Name
				stateGroup = st.GroupName
			}
		} else if stMap, ok := it["state"].(map[string]interface{}); ok && stMap != nil {
			if sn, ok := stMap["name"].(string); ok && sn != "" {
				stateName = sn
			}
			if sg, ok := stMap["group"].(string); ok && sg != "" {
				stateGroup = sg
			}
		}

		item := issue.WorkItem{
			ID:         id,
			Key:        ticketKey,
			ProjectID:  projectID,
			ProjectKey: projectKey,
			SequenceID: seq,
			Title:      name,
			StateName:  stateName,
			StateGroup: stateGroup,
			State:      issue.State(stateGroup),
			Priority:   priority,
		}

		var assigneeIDs []string
		if assigneesArr, ok := it["assignees"].([]interface{}); ok && assigneesArr != nil {
			for _, a := range assigneesArr {
				if a == nil {
					continue
				}
				if aStr, ok := a.(string); ok && aStr != "" {
					assigneeIDs = append(assigneeIDs, aStr)
				} else if aMap, ok := a.(map[string]interface{}); ok && aMap != nil {
					if aID, ok := aMap["id"].(string); ok && aID != "" {
						assigneeIDs = append(assigneeIDs, aID)
					}
				}
			}
		}
		item.Assignees = assigneeIDs
		if len(assigneeIDs) > 0 {
			item.Assignee = assigneeIDs[0]
		}

		if labelsArr, ok := it["labels"].([]interface{}); ok && labelsArr != nil {
			var labelNames []string
			for _, l := range labelsArr {
				if l == nil {
					continue
				}
				if lStr, ok := l.(string); ok && lStr != "" {
					if lName, found := labelMap[lStr]; found {
						labelNames = append(labelNames, lName)
					} else {
						labelNames = append(labelNames, lStr)
					}
				} else if lMap, ok := l.(map[string]interface{}); ok && lMap != nil {
					if ln, ok := lMap["name"].(string); ok && ln != "" {
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
	if snapshots == nil {
		snapshots = make([]issue.IssueSnapshot, 0)
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

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error de red al recibir respuesta de Plane: %w", err)
	}

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		var respPayload map[string]interface{}
		commentID := ""
		if json.Unmarshal(respBytes, &respPayload) == nil && respPayload != nil {
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

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error de red al recibir respuesta de creación: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("Plane retornó HTTP %d al crear tarea: %s", resp.StatusCode, string(respBytes))
	}

	var created map[string]interface{}
	if err := json.Unmarshal(respBytes, &created); err != nil {
		return nil, fmt.Errorf("error decodificando tarea creada: %w", err)
	}
	if created == nil {
		return nil, fmt.Errorf("respuesta vacía al crear tarea en Plane")
	}

	id, _ := created["id"].(string)
	seq := parseSequenceID(created["sequence_id"])
	ticketKey := fmt.Sprintf("%s-%d", input.ProjectKey, seq)
	if seq == 0 && id != "" {
		ticketKey = fmt.Sprintf("%s-%s", input.ProjectKey, id)
	}

	return &issue.WorkItem{
		ID:          id,
		Key:         ticketKey,
		ProjectID:   projectID,
		ProjectKey:  input.ProjectKey,
		SequenceID:  seq,
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
	var wrapper struct {
		Results []map[string]interface{} `json:"results"`
	}
	if err := json.Unmarshal(respBytes, &wrapper); err == nil && len(wrapper.Results) > 0 {
		res = wrapper.Results
	} else {
		_ = json.Unmarshal(respBytes, &res)
	}

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

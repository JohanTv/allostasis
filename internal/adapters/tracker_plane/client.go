package tracker_plane

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/domain/issue"
)

// Config almacena las credenciales y URLs de conexión a Plane CE.
type Config struct {
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"api_key"`
	Workspace string `json:"workspace"`
	Timeout   time.Duration
}

// UserInfo representa los metadatos de usuario devueltos por Plane CE.
type UserInfo struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

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

// Client gestiona el transporte HTTP seguro con la API REST de Plane CE.
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

// SyncReport resume las operaciones ejecutadas durante el ciclo de sincronización.
type SyncReport struct {
	SentComments   int      `json:"sent_comments"`
	FailedComments int      `json:"failed_comments"`
	SyncedIssues   int      `json:"synced_issues"`
	Errors         []string `json:"errors,omitempty"`
	Status         string   `json:"status"`
}

// FlushOutbox publica los comentarios y registros pendientes en la cola outbox hacia Plane.
func (c *Client) FlushOutbox(ctx context.Context, db *sql.DB, limit int) (*SyncReport, error) {
	report := &SyncReport{Status: "completed"}
	if limit <= 0 {
		limit = 20
	}

	if c.cfg.APIKey == "" {
		report.Status = "skipped_no_credentials"
		report.Errors = append(report.Errors, "PLANE_API_KEY no configurada en el entorno: omitiendo flush remoto hacia Plane")
		return report, nil
	}

	query := `SELECT id, client_id, ticket_ref, payload_json, COALESCE(target_workspace, ?)
	          FROM outbox WHERE state IN ('pending', 'failed') ORDER BY id ASC LIMIT ?`
	rows, err := db.QueryContext(ctx, query, c.cfg.Workspace, limit)
	if err != nil {
		return nil, fmt.Errorf("error al consultar cola outbox: %w", err)
	}
	defer rows.Close()

	type outboxRow struct {
		id          int64
		clientID    string
		ticketRef   string
		payloadJSON string
		targetWS    string
	}
	var pending []outboxRow

	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.id, &r.clientID, &r.ticketRef, &r.payloadJSON, &r.targetWS); err != nil {
			continue
		}
		pending = append(pending, r)
	}

	for _, item := range pending {
		// Obtener project_id y issue UUID para el ticket_ref
		var issueID, projectID string
		lookupQuery := `SELECT id, project_id FROM issues_cache WHERE ticket_id = ? LIMIT 1`
		err := db.QueryRowContext(ctx, lookupQuery, item.ticketRef).Scan(&issueID, &projectID)
		if err != nil {
			report.FailedComments++
			errMsg := fmt.Sprintf("no se encontró issue ID para %s: %v", item.ticketRef, err)
			report.Errors = append(report.Errors, errMsg)
			_, _ = db.ExecContext(ctx, `UPDATE outbox SET state = 'failed', last_error = ?, attempts = attempts + 1 WHERE id = ?`, errMsg, item.id)
			continue
		}

		// URL canónica de comentarios en Plane CE
		url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issues/%s/comments/", c.cfg.BaseURL, item.targetWS, projectID, issueID)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString(item.payloadJSON))
		if err != nil {
			report.FailedComments++
			report.Errors = append(report.Errors, err.Error())
			continue
		}

		req.Header.Set("x-api-key", c.cfg.APIKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			report.FailedComments++
			errMsg := fmt.Sprintf("error de red al enviar a Plane: %v", err)
			report.Errors = append(report.Errors, errMsg)
			_, _ = db.ExecContext(ctx, `UPDATE outbox SET state = 'failed', last_error = ?, attempts = attempts + 1 WHERE id = ?`, errMsg, item.id)
			continue
		}

		respBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
			var respPayload map[string]interface{}
			commentID := ""
			if json.Unmarshal(respBytes, &respPayload) == nil {
				if cid, ok := respPayload["id"].(string); ok {
					commentID = cid
				}
			}

			nowStr := time.Now().Format(time.RFC3339)
			_, _ = db.ExecContext(ctx, `UPDATE outbox SET state = 'sent', plane_comment_id = ?, sent_at = ?, attempts = attempts + 1 WHERE id = ?`, commentID, nowStr, item.id)
			report.SentComments++
		} else if resp.StatusCode == http.StatusUnauthorized {
			report.Status = "unauthorized"
			report.Errors = append(report.Errors, "Plane rechazó las credenciales (HTTP 401): verifica PLANE_API_KEY")
			return report, fmt.Errorf("autenticación fallida en Plane: HTTP 401")
		} else {
			report.FailedComments++
			errMsg := fmt.Sprintf("Plane retornó HTTP %d: %s", resp.StatusCode, string(respBytes))
			report.Errors = append(report.Errors, errMsg)
			_, _ = db.ExecContext(ctx, `UPDATE outbox SET state = 'failed', last_error = ?, attempts = attempts + 1 WHERE id = ?`, errMsg, item.id)
		}
	}

	return report, nil
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

// SyncProjects descarga el catálogo de proyectos del workspace y los persiste en SQLite.
func (c *Client) SyncProjects(ctx context.Context, store *sqlite.Store) ([]sqlite.ProjectRecord, error) {
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

	var records []sqlite.ProjectRecord
	for _, p := range rawProjects {
		id, _ := p["id"].(string)
		identifier, _ := p["identifier"].(string)
		name, _ := p["name"].(string)
		if id == "" || identifier == "" {
			continue
		}
		rec := sqlite.ProjectRecord{
			Identifier: strings.ToUpper(identifier),
			ProjectID:  id,
			Name:       name,
			Workspace:  c.cfg.Workspace,
		}
		if store != nil {
			_ = store.SaveProject(ctx, rec)
		}
		records = append(records, rec)
	}
	return records, nil
}

// SyncStates descarga los estados de un proyecto específico y los persiste en SQLite.
func (c *Client) SyncStates(ctx context.Context, store *sqlite.Store, projectID string) ([]sqlite.StateRecord, error) {
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

	var records []sqlite.StateRecord
	for _, s := range rawStates {
		id, _ := s["id"].(string)
		name, _ := s["name"].(string)
		slug, _ := s["slug"].(string)
		group, _ := s["group"].(string)
		if id == "" || name == "" {
			continue
		}
		rec := sqlite.StateRecord{
			ID:        id,
			ProjectID: projectID,
			Name:      name,
			Slug:      slug,
			GroupName: group,
		}
		if store != nil {
			_ = store.SaveState(ctx, rec)
		}
		records = append(records, rec)
	}
	return records, nil
}

// SyncIssues descarga las tareas actualizadas de un proyecto y las guarda en issues_cache e issue_snapshots.
func (c *Client) SyncIssues(ctx context.Context, store *sqlite.Store, projectKey string, limit int) (int, error) {
	if c.cfg.APIKey == "" {
		return 0, fmt.Errorf("PLANE_API_KEY no configurada")
	}
	projectKey = strings.ToUpper(strings.TrimSpace(projectKey))

	// Resolver projectID
	var projectID string
	pRec, err := store.GetProject(ctx, projectKey)
	if err == nil {
		projectID = pRec.ProjectID
	} else {
		// Intentar sincronizar proyectos
		projects, errSync := c.SyncProjects(ctx, store)
		if errSync != nil {
			return 0, fmt.Errorf("no se pudo resolver proyecto %s: %w", projectKey, errSync)
		}
		for _, p := range projects {
			if p.Identifier == projectKey {
				projectID = p.ProjectID
				break
			}
		}
	}
	if projectID == "" {
		return 0, fmt.Errorf("proyecto %s no encontrado en Plane", projectKey)
	}

	// Sincronizar estados asociados al proyecto
	_, _ = c.SyncStates(ctx, store, projectID)

	if limit <= 0 {
		limit = 100
	}
	url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issues/?order_by=-updated_at&per_page=%d", c.cfg.BaseURL, c.cfg.Workspace, projectID, limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("error de red al descargar tareas de Plane: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("Plane retornó HTTP %d al listar tareas: %s", resp.StatusCode, string(body))
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

	syncedCount := 0
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

		item := &issue.WorkItem{
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

		// Extraer labels
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

		if err := store.SaveIssue(ctx, item); err == nil {
			rawJSON, _ := json.Marshal(it)
			_ = store.SaveIssueSnapshot(ctx, id, ticketKey, string(rawJSON))
			syncedCount++
		}
	}

	return syncedCount, nil
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

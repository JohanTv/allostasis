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
)

// Config almacena las credenciales y URLs de conexión a Plane CE.
type Config struct {
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"api_key"`
	Workspace string `json:"workspace"`
	Timeout   time.Duration
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

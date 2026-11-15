package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/domain/ledger"
	_ "github.com/ncruces/go-sqlite3/driver"
)

// ProjectRecord modela un proyecto registrado en Plane/Allostasis.
type ProjectRecord struct {
	Identifier string `json:"identifier"`
	ProjectID  string `json:"project_id"`
	Name       string `json:"name"`
	Workspace  string `json:"workspace"`
}

// StateRecord modela un estado de ticket por proyecto.
type StateRecord struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	GroupName string `json:"group_name"`
}

// Store implementa los puertos de lectura y escritura de tareas y ledger sobre SQLite (Wazero pure-Go).
type Store struct {
	db *sql.DB
}

// NewStore inicializa la conexión SQLite con WAL mode, timeout de 10s y baseline migration automático.
func NewStore(dsn string) (*Store, error) {
	var uri string
	if dsn == ":memory:" {
		uri = "file:alo_shared_mem?mode=memory&cache=shared&_pragma=busy_timeout(10000)"
	} else if len(dsn) > 5 && dsn[:5] == "file:" {
		uri = dsn
	} else {
		uri = fmt.Sprintf("file:%s?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)", dsn)
	}

	db, err := sql.Open("sqlite3", uri)
	if err != nil {
		return nil, fmt.Errorf("error al abrir base de datos sqlite: %w", err)
	}

	// Configuración de pool
	if dsn == ":memory:" {
		// Bases de datos en memoria requieren conexión fija única para preservar tablas del schema
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	} else {
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(time.Hour)
	}

	s := &Store{db: db}
	if err := s.migrateBaseline(); err != nil {
		db.Close()
		return nil, fmt.Errorf("error al ejecutar migraciones baseline: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// DB expone la conexión subyacente para operaciones transaccionales o sincronizadores.
func (s *Store) DB() *sql.DB {
	return s.db
}

// migrateBaseline asegura que las tablas requeridas existan sin romper bases preexistentes de Python.
func (s *Store) migrateBaseline() error {
	schema := `
	CREATE TABLE IF NOT EXISTS worklogs (
		ticket_id TEXT NOT NULL,
		track TEXT NOT NULL DEFAULT 'main',
		date TEXT NOT NULL,
		start_time TEXT NOT NULL,
		end_time TEXT NOT NULL,
		duration_minutes INTEGER NOT NULL CHECK (duration_minutes >= 0),
		plane_comment_id TEXT,
		project_id TEXT NOT NULL DEFAULT '',
		issue_name TEXT NOT NULL DEFAULT '',
		worker_type TEXT NOT NULL DEFAULT 'human',
		capacity_bucket TEXT NOT NULL DEFAULT 'deep_work',
		phase TEXT NOT NULL DEFAULT 'construction_dev',
		role TEXT NOT NULL DEFAULT 'backend_engineer',
		notes TEXT NOT NULL DEFAULT '',
		stoppers_json TEXT NOT NULL DEFAULT '[]',
		raw_payload TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL,
		updated_at REAL NOT NULL,
		author_id TEXT,
		defaulted_json TEXT NOT NULL DEFAULT '[]',
		PRIMARY KEY (ticket_id, track, date, start_time)
	);

	CREATE INDEX IF NOT EXISTS idx_worklogs_date_start ON worklogs(date, start_time);
	CREATE INDEX IF NOT EXISTS idx_worklogs_ticket_track ON worklogs(ticket_id, track);

	CREATE TABLE IF NOT EXISTS issues_cache (
		id TEXT PRIMARY KEY,
		sequence_id INTEGER NOT NULL DEFAULT 0,
		ticket_id TEXT NOT NULL UNIQUE,
		project_id TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL,
		state_name TEXT NOT NULL DEFAULT 'Backlog',
		state_group TEXT NOT NULL DEFAULT 'backlog',
		priority TEXT NOT NULL DEFAULT 'none',
		labels_csv TEXT NOT NULL DEFAULT '',
		assignees_json TEXT NOT NULL DEFAULT '[]',
		created_at TEXT NOT NULL DEFAULT '',
		updated_at REAL NOT NULL DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_issues_ticket ON issues_cache(ticket_id);
	CREATE INDEX IF NOT EXISTS idx_issues_group ON issues_cache(state_group);

	CREATE TABLE IF NOT EXISTS projects (
		identifier TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		name TEXT NOT NULL,
		workspace TEXT NOT NULL DEFAULT 'teamprogra',
		updated_at REAL NOT NULL DEFAULT 0,
		is_member INTEGER NOT NULL DEFAULT 1,
		archived INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS states (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		name TEXT NOT NULL,
		slug TEXT NOT NULL,
		group_name TEXT NOT NULL,
		is_default INTEGER NOT NULL DEFAULT 0,
		updated_at REAL NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS labels (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		name TEXT NOT NULL,
		color TEXT NOT NULL DEFAULT '',
		updated_at REAL NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS issue_snapshots (
		issue_id TEXT PRIMARY KEY,
		ticket_id TEXT NOT NULL,
		fetched_at REAL NOT NULL DEFAULT 0,
		issue_json TEXT NOT NULL DEFAULT '{}',
		comments_json TEXT DEFAULT '[]',
		comments_fetched_at REAL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS outbox (
		id TEXT PRIMARY KEY,
		work_item TEXT NOT NULL,
		payload TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		retries INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL
	);
	`
	_, err := s.db.Exec(schema)
	return err
}

// GetIssue consulta una tarea por su clave (ej. OMNIFY-175) o UUID, extrayendo también su descripción si existe.
func (s *Store) GetIssue(ctx context.Context, key string) (*issue.WorkItem, error) {
	key = strings.TrimSpace(strings.ToUpper(key))
	query := `SELECT id, ticket_id, project_id, title, state_name, COALESCE(state_group, state_name), priority, COALESCE(assignees_json, '')
	          FROM issues_cache WHERE ticket_id = ? OR id = ?`
	row := s.db.QueryRowContext(ctx, query, key, key)

	var item issue.WorkItem
	var stateStr string
	err := row.Scan(&item.ID, &item.Key, &item.ProjectID, &item.Title, &item.StateName, &stateStr, &item.Priority, &item.Assignee)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("tarea no encontrada: %s", key)
		}
		return nil, err
	}
	item.State = issue.State(stateStr)
	item.StateGroup = stateStr

	// Enriquecer con descripción desde issue_snapshots si existe
	var snapshotJSON sql.NullString
	_ = s.db.QueryRowContext(ctx, `SELECT issue_json FROM issue_snapshots WHERE ticket_id = ? OR issue_id = ?`, item.Key, item.ID).Scan(&snapshotJSON)
	if snapshotJSON.Valid && snapshotJSON.String != "" {
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(snapshotJSON.String), &raw); err == nil {
			if d, ok := raw["description_html"].(string); ok && d != "" {
				item.DescriptionHTML = d
			}
			if d, ok := raw["description"].(string); ok && d != "" {
				item.Description = d
			} else if d, ok := raw["description_stripped"].(string); ok && d != "" {
				item.Description = d
			}
		}
	}

	return &item, nil
}

// ListIssues lista tareas por estado.
func (s *Store) ListIssues(ctx context.Context, state issue.State) ([]issue.WorkItem, error) {
	var query string
	var rows *sql.Rows
	var err error

	if state != "" {
		query = `SELECT id, ticket_id, project_id, title, state_name, COALESCE(state_group, state_name), priority, COALESCE(assignees_json, '')
		         FROM issues_cache WHERE state_group = ? OR state_name = ? ORDER BY ticket_id ASC`
		rows, err = s.db.QueryContext(ctx, query, state, state)
	} else {
		query = `SELECT id, ticket_id, project_id, title, state_name, COALESCE(state_group, state_name), priority, COALESCE(assignees_json, '')
		         FROM issues_cache ORDER BY ticket_id ASC`
		rows, err = s.db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []issue.WorkItem
	for rows.Next() {
		var item issue.WorkItem
		var stateStr string
		if err := rows.Scan(&item.ID, &item.Key, &item.ProjectID, &item.Title, &item.StateName, &stateStr, &item.Priority, &item.Assignee); err != nil {
			return nil, err
		}
		item.State = issue.State(stateStr)
		item.StateGroup = stateStr
		list = append(list, item)
	}
	return list, nil
}

// ListIssuesFiltered lista tareas aplicando filtros flexibles de proyecto, estado, consulta y límite.
func (s *Store) ListIssuesFiltered(ctx context.Context, filter issue.IssueFilter) ([]issue.WorkItem, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	q := `SELECT id, ticket_id, project_id, title, state_name, COALESCE(state_group, state_name), priority, COALESCE(assignees_json, '')
	      FROM issues_cache WHERE 1=1`
	var args []interface{}

	if filter.ProjectKey != "" {
		pKey := strings.ToUpper(strings.TrimSpace(filter.ProjectKey))
		q += ` AND (project_id = ? OR ticket_id LIKE ?)`
		args = append(args, pKey, pKey+"-%")
	}
	if filter.State != "" {
		q += ` AND (state_name = ? OR state_group = ?)`
		args = append(args, filter.State, filter.State)
	}
	if filter.StateGroup != "" {
		q += ` AND state_group = ?`
		args = append(args, filter.StateGroup)
	}
	if filter.Query != "" {
		likePattern := "%" + filter.Query + "%"
		q += ` AND (title LIKE ? OR ticket_id LIKE ?)`
		args = append(args, likePattern, likePattern)
	}

	q += ` ORDER BY ticket_id DESC LIMIT ? OFFSET ?`
	args = append(args, filter.Limit, filter.Offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []issue.WorkItem
	for rows.Next() {
		var item issue.WorkItem
		var stateStr string
		if err := rows.Scan(&item.ID, &item.Key, &item.ProjectID, &item.Title, &item.StateName, &stateStr, &item.Priority, &item.Assignee); err != nil {
			return nil, err
		}
		item.State = issue.State(stateStr)
		item.StateGroup = stateStr
		list = append(list, item)
	}
	return list, nil
}

// SearchIssues realiza una búsqueda tolerante en título, clave, etiquetas Y descripción completa (evitando falsos negativos).
func (s *Store) SearchIssues(ctx context.Context, query string, project string, limit int) ([]issue.WorkItem, error) {
	if limit <= 0 {
		limit = 20
	}
	likePattern := "%" + strings.TrimSpace(query) + "%"

	q := `
		SELECT c.id, c.ticket_id, c.project_id, c.title, c.state_name, COALESCE(c.state_group, c.state_name), c.priority,
		       COALESCE(c.assignees_json, ''), COALESCE(s.issue_json, '')
		FROM issues_cache c
		LEFT JOIN issue_snapshots s ON (c.id = s.issue_id OR c.ticket_id = s.ticket_id)
		WHERE (c.title LIKE ? OR c.ticket_id LIKE ? OR c.labels_csv LIKE ? OR s.issue_json LIKE ?)
	`
	args := []interface{}{likePattern, likePattern, likePattern, likePattern}

	if project != "" {
		pKey := strings.ToUpper(strings.TrimSpace(project))
		q += ` AND (c.project_id = ? OR c.ticket_id LIKE ?)`
		args = append(args, pKey, pKey+"-%")
	}

	q += ` ORDER BY c.updated_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []issue.WorkItem
	for rows.Next() {
		var item issue.WorkItem
		var stateStr, snapshotJSON string
		if err := rows.Scan(&item.ID, &item.Key, &item.ProjectID, &item.Title, &item.StateName, &stateStr, &item.Priority, &item.Assignee, &snapshotJSON); err != nil {
			return nil, err
		}
		item.State = issue.State(stateStr)
		item.StateGroup = stateStr

		if snapshotJSON != "" {
			var raw map[string]interface{}
			if err := json.Unmarshal([]byte(snapshotJSON), &raw); err == nil {
				if d, ok := raw["description_html"].(string); ok && d != "" {
					item.DescriptionHTML = d
				}
				if d, ok := raw["description"].(string); ok && d != "" {
					item.Description = d
				} else if d, ok := raw["description_stripped"].(string); ok && d != "" {
					item.Description = d
				}
			}
		}
		list = append(list, item)
	}
	return list, nil
}

// SaveIssueSnapshot almacena el payload crudo JSON de una tarea para búsquedas en texto completo.
func (s *Store) SaveIssueSnapshot(ctx context.Context, issueID, ticketID, issueJSON string) error {
	nowUnix := float64(time.Now().Unix())
	query := `INSERT INTO issue_snapshots(issue_id, ticket_id, fetched_at, issue_json)
	          VALUES(?, ?, ?, ?)
	          ON CONFLICT(issue_id) DO UPDATE SET
	            ticket_id=excluded.ticket_id,
	            fetched_at=excluded.fetched_at,
	            issue_json=excluded.issue_json`
	_, err := s.db.ExecContext(ctx, query, issueID, ticketID, nowUnix, issueJSON)
	return err
}

// SaveProject almacena o actualiza la definición de un proyecto.
func (s *Store) SaveProject(ctx context.Context, p ProjectRecord) error {
	nowUnix := float64(time.Now().Unix())
	query := `INSERT INTO projects(identifier, project_id, name, workspace, updated_at)
	          VALUES(?, ?, ?, ?, ?)
	          ON CONFLICT(identifier) DO UPDATE SET
	            project_id=excluded.project_id,
	            name=excluded.name,
	            workspace=excluded.workspace,
	            updated_at=excluded.updated_at`
	_, err := s.db.ExecContext(ctx, query, p.Identifier, p.ProjectID, p.Name, p.Workspace, nowUnix)
	return err
}

// ListProjects retorna todos los proyectos registrados en la caché local.
func (s *Store) ListProjects(ctx context.Context) ([]ProjectRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT identifier, project_id, name, workspace FROM projects ORDER BY identifier ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []ProjectRecord
	for rows.Next() {
		var p ProjectRecord
		if err := rows.Scan(&p.Identifier, &p.ProjectID, &p.Name, &p.Workspace); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, nil
}

// GetProject busca un proyecto por su identificador (ej. OMNIFY, OPS).
func (s *Store) GetProject(ctx context.Context, identifier string) (*ProjectRecord, error) {
	identifier = strings.ToUpper(strings.TrimSpace(identifier))
	var p ProjectRecord
	err := s.db.QueryRowContext(ctx, `SELECT identifier, project_id, name, workspace FROM projects WHERE identifier = ? OR project_id = ?`, identifier, identifier).
		Scan(&p.Identifier, &p.ProjectID, &p.Name, &p.Workspace)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("proyecto no encontrado: %s", identifier)
		}
		return nil, err
	}
	return &p, nil
}

// SaveState almacena la definición de un estado por proyecto.
func (s *Store) SaveState(ctx context.Context, st StateRecord) error {
	nowUnix := float64(time.Now().Unix())
	query := `INSERT INTO states(id, project_id, name, slug, group_name, updated_at)
	          VALUES(?, ?, ?, ?, ?, ?)
	          ON CONFLICT(id) DO UPDATE SET
	            project_id=excluded.project_id,
	            name=excluded.name,
	            slug=excluded.slug,
	            group_name=excluded.group_name,
	            updated_at=excluded.updated_at`
	_, err := s.db.ExecContext(ctx, query, st.ID, st.ProjectID, st.Name, st.Slug, st.GroupName, nowUnix)
	return err
}

// GetState busca un estado por su nombre en un proyecto específico.
func (s *Store) GetState(ctx context.Context, projectID, name string) (*StateRecord, error) {
	var st StateRecord
	err := s.db.QueryRowContext(ctx, `SELECT id, project_id, name, slug, group_name FROM states WHERE project_id = ? AND (LOWER(name) = LOWER(?) OR LOWER(slug) = LOWER(?))`, projectID, name, name).
		Scan(&st.ID, &st.ProjectID, &st.Name, &st.Slug, &st.GroupName)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("estado '%s' no encontrado para el proyecto", name)
		}
		return nil, err
	}
	return &st, nil
}

// SaveIssue inserta o actualiza una tarea en la caché local.
func (s *Store) SaveIssue(ctx context.Context, item *issue.WorkItem) error {
	query := `INSERT INTO issues_cache(id, sequence_id, ticket_id, project_id, title, state_name, state_group, priority, labels_csv, assignees_json, updated_at)
	          VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	          ON CONFLICT(id) DO UPDATE SET
	            sequence_id=excluded.sequence_id,
	            ticket_id=excluded.ticket_id,
	            project_id=excluded.project_id,
	            title=excluded.title,
	            state_name=excluded.state_name,
	            state_group=excluded.state_group,
	            priority=excluded.priority,
	            labels_csv=excluded.labels_csv,
	            assignees_json=excluded.assignees_json,
	            updated_at=excluded.updated_at`

	nowUnix := float64(time.Now().Unix())
	stateName := item.StateName
	if stateName == "" {
		stateName = "Backlog"
	}
	stateGroup := item.StateGroup
	if stateGroup == "" {
		stateGroup = string(item.State)
		if stateGroup == "" {
			stateGroup = "backlog"
		}
	}
	priority := item.Priority
	if priority == "" {
		priority = "none"
	}

	_, err := s.db.ExecContext(ctx, query, item.ID, item.SequenceID, item.Key, item.ProjectID, item.Title, stateName, stateGroup, priority, item.LabelsCSV, item.Assignee, nowUnix)
	return err
}

// EnqueueOutbox registra una mutación diferida hacia el tracker upstream.
func (s *Store) EnqueueOutbox(ctx context.Context, event *issue.OutboxEvent) error {
	query := `INSERT INTO outbox(id, work_item, payload, status, retries, created_at)
	          VALUES(?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, event.ID, event.WorkItem, event.Payload, event.Status, event.Retries, event.CreatedAt.Format(time.RFC3339))
	return err
}

// SaveWorklog persiste un registro atómico de esfuerzo en el ledger.
func (s *Store) SaveWorklog(ctx context.Context, entry *ledger.WorklogEntry) error {
	query := `INSERT OR REPLACE INTO worklogs(ticket_id, track, date, start_time, end_time, duration_minutes, capacity_bucket, notes, created_at, updated_at)
	          VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	dateStr := entry.Interval.Start.Format("2006-01-02")
	startTimeStr := entry.Interval.Start.Format("15:04:05")
	endTimeStr := entry.Interval.End.Format("15:04:05")
	durationMins := entry.Interval.Minutes()
	nowUnix := float64(time.Now().Unix())

	_, err := s.db.ExecContext(ctx, query,
		entry.IssueID,
		"main",
		dateStr,
		startTimeStr,
		endTimeStr,
		durationMins,
		string(entry.Bucket),
		entry.Topic,
		time.Now().Format(time.RFC3339),
		nowUnix,
	)
	return err
}

func parseDateTimeFlexible(dateStr, timeStr string) (time.Time, error) {
	dateStr = strings.TrimSpace(dateStr)
	timeStr = strings.TrimSpace(timeStr)
	if len(timeStr) == 5 {
		return time.Parse("2006-01-02 15:04", dateStr+" "+timeStr)
	}
	return time.Parse("2006-01-02 15:04:05", dateStr+" "+timeStr)
}

// GetIntervals extrae los intervalos de tiempo del ledger para el cálculo continuo de Klee.
func (s *Store) GetIntervals(ctx context.Context) ([]ledger.Interval, error) {
	query := `SELECT date, start_time, end_time FROM worklogs ORDER BY date ASC, start_time ASC`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var intervals []ledger.Interval
	for rows.Next() {
		var dateStr, startStr, endStr string
		if err := rows.Scan(&dateStr, &startStr, &endStr); err != nil {
			return nil, err
		}

		start, err1 := parseDateTimeFlexible(dateStr, startStr)
		end, err2 := parseDateTimeFlexible(dateStr, endStr)
		if err1 == nil && err2 == nil && !end.Before(start) {
			intervals = append(intervals, ledger.Interval{Start: start, End: end})
		}
	}
	return intervals, nil
}

// GetWorklogs recupera los registros de trabajo filtrados opcionalmente por bucket de capacidad.
func (s *Store) GetWorklogs(ctx context.Context, filter ledger.CapacityBucket) ([]ledger.WorklogEntry, error) {
	var query string
	var rows *sql.Rows
	var err error

	if filter != "" {
		query = `SELECT ticket_id, date, start_time, end_time, duration_minutes, capacity_bucket, notes, created_at
		         FROM worklogs WHERE capacity_bucket = ? ORDER BY date DESC, start_time DESC`
		rows, err = s.db.QueryContext(ctx, query, string(filter))
	} else {
		query = `SELECT ticket_id, date, start_time, end_time, duration_minutes, capacity_bucket, notes, created_at
		         FROM worklogs ORDER BY date DESC, start_time DESC`
		rows, err = s.db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []ledger.WorklogEntry
	for rows.Next() {
		var e ledger.WorklogEntry
		var dateStr, startStr, endStr, bucketStr, createdAtStr string
		var durationMins int

		if err := rows.Scan(&e.IssueID, &dateStr, &startStr, &endStr, &durationMins, &bucketStr, &e.Topic, &createdAtStr); err != nil {
			return nil, err
		}

		start, _ := parseDateTimeFlexible(dateStr, startStr)
		end, _ := parseDateTimeFlexible(dateStr, endStr)
		e.Interval = ledger.Interval{Start: start, End: end}
		e.Bucket = ledger.CapacityBucket(bucketStr)
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		list = append(list, e)
	}
	return list, nil
}

package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/domain/ledger"
	_ "github.com/ncruces/go-sqlite3/driver"
)

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
		ticket_id TEXT NOT NULL UNIQUE,
		project_id TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL,
		state_name TEXT NOT NULL DEFAULT 'Backlog',
		state_group TEXT NOT NULL DEFAULT 'backlog',
		priority TEXT NOT NULL DEFAULT 'none',
		assignees_json TEXT NOT NULL DEFAULT '[]',
		created_at TEXT NOT NULL DEFAULT '',
		updated_at REAL NOT NULL DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_issues_ticket ON issues_cache(ticket_id);
	CREATE INDEX IF NOT EXISTS idx_issues_group ON issues_cache(state_group);

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

// GetIssue consulta una tarea por su clave (ej. OMNIFY-175).
func (s *Store) GetIssue(ctx context.Context, key string) (*issue.WorkItem, error) {
	query := `SELECT id, ticket_id, title, COALESCE(state_group, state_name), priority, COALESCE(assignees_json, '')
	          FROM issues_cache WHERE ticket_id = ?`
	row := s.db.QueryRowContext(ctx, query, key)

	var item issue.WorkItem
	var stateStr string
	err := row.Scan(&item.ID, &item.Key, &item.Title, &stateStr, &item.Priority, &item.Assignee)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("tarea no encontrada: %s", key)
		}
		return nil, err
	}
	item.State = issue.State(stateStr)
	return &item, nil
}

// ListIssues lista tareas por estado.
func (s *Store) ListIssues(ctx context.Context, state issue.State) ([]issue.WorkItem, error) {
	var query string
	var rows *sql.Rows
	var err error

	if state != "" {
		query = `SELECT id, ticket_id, title, COALESCE(state_group, state_name), priority, COALESCE(assignees_json, '')
		         FROM issues_cache WHERE state_group = ? OR state_name = ? ORDER BY ticket_id ASC`
		rows, err = s.db.QueryContext(ctx, query, state, state)
	} else {
		query = `SELECT id, ticket_id, title, COALESCE(state_group, state_name), priority, COALESCE(assignees_json, '')
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
		if err := rows.Scan(&item.ID, &item.Key, &item.Title, &stateStr, &item.Priority, &item.Assignee); err != nil {
			return nil, err
		}
		item.State = issue.State(stateStr)
		list = append(list, item)
	}
	return list, nil
}

// SaveIssue inserta o actualiza una tarea en la caché local.
func (s *Store) SaveIssue(ctx context.Context, item *issue.WorkItem) error {
	query := `INSERT INTO issues_cache(id, ticket_id, title, state_name, state_group, priority, assignees_json, updated_at)
	          VALUES(?, ?, ?, ?, ?, ?, ?, ?)
	          ON CONFLICT(id) DO UPDATE SET
	            ticket_id=excluded.ticket_id,
	            title=excluded.title,
	            state_name=excluded.state_name,
	            state_group=excluded.state_group,
	            priority=excluded.priority,
	            assignees_json=excluded.assignees_json,
	            updated_at=excluded.updated_at`

	nowUnix := float64(time.Now().Unix())
	stateStr := string(item.State)
	if stateStr == "" {
		stateStr = "backlog"
	}

	_, err := s.db.ExecContext(ctx, query, item.ID, item.Key, item.Title, stateStr, stateStr, item.Priority, item.Assignee, nowUnix)
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

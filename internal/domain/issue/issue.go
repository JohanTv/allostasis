package issue

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// UnknownLabelError indica etiquetas que no existen en el proyecto: es un error de entrada,
// no transitorio, así que no debe encolarse en outbox.
type UnknownLabelError struct {
	Names []string
	Valid []string
}

func (e *UnknownLabelError) Error() string {
	return fmt.Sprintf("etiqueta(s) desconocida(s): %s (válidas: %s)", strings.Join(e.Names, ", "), strings.Join(e.Valid, ", "))
}

// RejectedError indica que Plane rechazó la mutación por datos inválidos (400, 404, 409, 422).
// Reintentarla no la arregla, así que no se encola ni se descarta en silencio: se muestra al usuario.
type RejectedError struct {
	Status    int
	Operation string
	Detail    string
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("Plane retornó HTTP %d al %s: %s", e.Status, e.Operation, e.Detail)
}

// IsInputError reporta si err es un rechazo que debe propagarse en vez de encolarse en outbox.
func IsInputError(err error) bool {
	var unknown *UnknownLabelError
	var rejected *RejectedError
	return errors.As(err, &unknown) || errors.As(err, &rejected)
}

// State representa el grupo canónico alostático y de ciclo de vida de una tarea.
type State string

const (
	StateBacklog   State = "backlog"
	StateUnstarted State = "unstarted"
	StateStarted   State = "started"
	StateCompleted State = "completed"
	StateCancelled State = "cancelled"
)

// WorkItem representa un ticket o elemento de trabajo en el tracker de Allostasis.
type WorkItem struct {
	ID              string     `json:"id"`
	Key             string     `json:"key"` // ej. OMNIFY-175, PROJ-10
	ProjectID       string     `json:"project_id,omitempty"`
	ProjectKey      string     `json:"project_key,omitempty"`
	SequenceID      int        `json:"sequence_id,omitempty"`
	Title           string     `json:"title"`
	Description     string     `json:"description,omitempty"`
	DescriptionHTML string     `json:"description_html,omitempty"`
	State           State      `json:"state"`
	StateName       string     `json:"state_name,omitempty"`
	StateGroup      string     `json:"state_group,omitempty"`
	Priority        string     `json:"priority"`
	Assignee        string     `json:"assignee,omitempty"`
	Assignees       []string   `json:"assignees,omitempty"`
	CreatedBy       string     `json:"created_by,omitempty"`
	Labels          []string   `json:"labels,omitempty"`
	LabelsCSV       string     `json:"labels_csv,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	StartDate       string     `json:"start_date,omitempty"`
	TargetDate      string     `json:"target_date,omitempty"`
	ArchivedAt      *time.Time `json:"archived_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	RemoteUpdatedAt *time.Time `json:"remote_updated_at,omitempty"`
}

// CreateIssueInput encapsula los datos requeridos para crear una tarea.
type CreateIssueInput struct {
	ProjectKey  string   `json:"project_key"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Priority    string   `json:"priority,omitempty"`
	StateName   string   `json:"state_name,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	StartDate   string   `json:"start_date,omitempty"`
	TargetDate  string   `json:"target_date,omitempty"`
}

// UpdateIssueInput encapsula las mutaciones permitidas sobre una tarea existente.
type UpdateIssueInput struct {
	Title       *string   `json:"title,omitempty"`
	Description *string   `json:"description,omitempty"`
	StateName   *string   `json:"state_name,omitempty"`
	Priority    *string   `json:"priority,omitempty"`
	Assignee    *string   `json:"assignee,omitempty"`
	Labels      *[]string `json:"labels,omitempty"`
	StartDate   *string   `json:"start_date,omitempty"`
	TargetDate  *string   `json:"target_date,omitempty"`
}

// IssueFilter define criterios de consulta y paginación.
type IssueFilter struct {
	ProjectKey   string
	State        string
	StateGroup   string
	Assignee     string
	CreatedBy    string
	InvolvedBy   string
	OpenOnly     bool
	ClosedOnly   bool
	ArchivedOnly bool
	Query        string
	Limit        int
	Offset       int
}

// OutboxStatus modela la máquina de estados de sincronización idempotente del Outbox.
type OutboxStatus string

const (
	OutboxPending   OutboxStatus = "pending"
	OutboxDelivered OutboxStatus = "delivered"
	OutboxFailed    OutboxStatus = "failed"
)

// OutboxEvent representa una mutación local diferida hacia el tracker upstream.
type OutboxEvent struct {
	ID        string       `json:"id"`
	WorkItem  string       `json:"work_item"`
	Payload   string       `json:"payload"`
	Status    OutboxStatus `json:"status"`
	Retries   int          `json:"retries"`
	CreatedAt time.Time    `json:"created_at"`
}

// Label modela una etiqueta en Plane CE / local.
type Label struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
}

// Link modela un hipervínculo externo o PR asociado a un ticket.
type Link struct {
	ID        string    `json:"id"`
	IssueID   string    `json:"issue_id"`
	TicketID  string    `json:"ticket_id"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

// WorklogRecord modela un registro resumido de tiempo asociado a un ticket para comprobaciones de integridad.
type WorklogRecord struct {
	PlaneCommentID  string `json:"plane_comment_id,omitempty"`
	DurationMinutes int    `json:"duration_minutes"`
	AuthorID        string `json:"author_id,omitempty"`
}

// CommentRecord representa un comentario remoto leído desde el tracker.
type CommentRecord struct {
	ID          string `json:"id"`
	CommentHTML string `json:"comment_html"`
	Actor       string `json:"actor,omitempty"`
	CreatedBy   string `json:"created_by,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// AssignmentEvent modela una mutación o registro histórico de asignación sobre un ticket.
type AssignmentEvent struct {
	EventID    string  `json:"event_id"`
	IssueID    string  `json:"issue_id"`
	TicketID   string  `json:"ticket_id"`
	AtUTC      string  `json:"at_utc"`
	Kind       string  `json:"kind"`
	UserID     string  `json:"user_id"`
	UserName   string  `json:"user_name,omitempty"`
	RecordedAt float64 `json:"recorded_at"`
}

// TicketHistory consolida la historia temporal de una tarea (asignaciones y sesiones de esfuerzo).
type TicketHistory struct {
	TicketID    string            `json:"ticket_id"`
	Assignments []AssignmentEvent `json:"assignments"`
	Worklogs    []WorklogRecord   `json:"worklogs"`
}

// Attachment modela un archivo adjunto subido a Plane CE.
type Attachment struct {
	ID        string    `json:"id"`
	IssueID   string    `json:"issue_id"`
	TicketID  string    `json:"ticket_id"`
	Name      string    `json:"name"`
	SizeBytes int64     `json:"size_bytes"`
	AssetURL  string    `json:"asset_url"`
	CreatedAt time.Time `json:"created_at"`
}

// FocusRecord registra el foco de atención activo de la sesión del agente.
type FocusRecord struct {
	ID              int64      `json:"id"`
	TicketID        string     `json:"ticket_id"`
	SessionID       string     `json:"session_id,omitempty"`
	StartTime       time.Time  `json:"start_time"`
	EndTime         *time.Time `json:"end_time,omitempty"`
	Active          bool       `json:"active"`
	DurationMinutes int        `json:"duration_minutes"`
	CreatedAt       time.Time  `json:"created_at"`
}

// WorklogProposal encapsula una recomendación de tiempo para registrar al cerrar sesión.
type WorklogProposal struct {
	TicketID        string    `json:"ticket_id"`
	DurationMinutes int       `json:"duration_minutes"`
	WindowStart     time.Time `json:"window_start"`
	WindowEnd       time.Time `json:"window_end"`
	CapacityBucket  string    `json:"capacity_bucket"`
	WorkerType      string    `json:"worker_type"`
	Reason          string    `json:"reason"`
}

// TimeReportItem resume el tiempo acumulado por ticket y balde.
type TimeReportItem struct {
	Date            string `json:"date"`
	Project         string `json:"project"`
	TicketKey       string `json:"ticket_key"`
	Title           string `json:"title"`
	DurationMinutes int    `json:"duration_minutes"`
	CapacityBucket  string `json:"capacity_bucket"`
	WorkerType      string `json:"worker_type"`
}

// TimeReport consolida la distribución de esfuerzo en un periodo.
type TimeReport struct {
	Period          string            `json:"period"`
	TotalMinutes    int               `json:"total_minutes"`
	TotalHours      float64           `json:"total_hours"`
	BucketBreakdown map[string]int    `json:"bucket_breakdown"`
	Items           []TimeReportItem  `json:"items"`
}

// FlowReport reporta métricas de lead time, cycle time y throughput.
type FlowReport struct {
	Period             string         `json:"period"`
	CompletedCount     int            `json:"completed_count"`
	AvgLeadTimeHours   float64        `json:"avg_lead_time_hours"`
	AvgCycleTimeHours  float64        `json:"avg_cycle_time_hours"`
	ThroughputPerWeek  float64        `json:"throughput_per_week"`
	StateDistribution  map[string]int `json:"state_distribution"`
}

// ProjectRecord modela un proyecto registrado en Plane/Allostasis.
type ProjectRecord struct {
	Identifier string `json:"identifier"`
	ProjectID  string `json:"project_id"`
	Name       string `json:"name"`
	Workspace  string `json:"workspace"`
}

// StateRecord modela un estado de ticket por proyecto.
type StateRecord struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	ProjectKey string `json:"project_key,omitempty"`
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	GroupName  string `json:"group_name"`
	IsDefault  bool   `json:"is_default,omitempty"`
}

// OutboxRecord representa una fila en la tabla outbox de despacho.
type OutboxRecord struct {
	ID              int64     `json:"id"`
	ClientID        string    `json:"client_id"`
	TicketRef       string    `json:"ticket_ref"`
	PayloadJSON     string    `json:"payload_json"`
	MutationType    string    `json:"mutation_type,omitempty"`
	State           string    `json:"state"`
	Attempts        int       `json:"attempts"`
	LastError       string    `json:"last_error,omitempty"`
	TargetWorkspace string    `json:"target_workspace,omitempty"`
	PlaneCommentID  string    `json:"plane_comment_id,omitempty"`
	SentAt          string    `json:"sent_at,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

// UserInfo modela los metadatos del usuario devueltos por el tracker remoto.
type UserInfo struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// SyncReport resume las operaciones ejecutadas durante el ciclo de sincronización.
type SyncReport struct {
	SentComments   int      `json:"sent_comments"`
	FailedComments int      `json:"failed_comments"`
	SyncedIssues   int      `json:"synced_issues"`
	Errors         []string `json:"errors,omitempty"`
	Status         string   `json:"status"`
}

// SyncWatermark registra la marca de agua y telemetría de sincronización incremental por dominio y proyecto (ADR 0028 / RFC 0027).
type SyncWatermark struct {
	Domain              string    `json:"domain"`
	ProjectID           string    `json:"project_id"`
	LastSyncTimestamp   float64   `json:"last_sync_timestamp"`
	ServerHighWatermark string    `json:"server_high_watermark,omitempty"`
	Status              string    `json:"status"`
	ItemsSynced         int       `json:"items_synced"`
	PagesFetched        int       `json:"pages_fetched"`
	DurationMs          int64     `json:"duration_ms"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// SyncUnifiedOptions encapsula los parámetros de la sincronización soberana y granular.
type SyncUnifiedOptions struct {
	Target      string `json:"target"` // all, tickets, catalogs, outbox, hours
	ProjectKey  string `json:"project_key,omitempty"`
	AllProjects bool   `json:"all_projects,omitempty"`
	Deep        bool   `json:"deep,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// SyncUnifiedReport resume las operaciones ejecutadas en la sincronización unificada.
type SyncUnifiedReport struct {
	Status         string            `json:"status"`
	Target         string            `json:"target"`
	SentComments   int               `json:"sent_comments"`
	FailedComments int               `json:"failed_comments"`
	SyncedIssues   int               `json:"synced_issues"`
	SyncedProjects int               `json:"synced_projects,omitempty"`
	SyncedCatalogs bool              `json:"synced_catalogs,omitempty"`
	Watermarks     map[string]string `json:"watermarks,omitempty"` // project -> server_high_watermark
	Errors         []string          `json:"errors,omitempty"`
	DurationMs     int64             `json:"duration_ms"`
}

// IssueSnapshot contiene la tarea parseada y su payload JSON original sin procesar.
type IssueSnapshot struct {
	Item    WorkItem `json:"item"`
	RawJSON string   `json:"raw_json"`
}

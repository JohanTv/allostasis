package issue

import (
	"time"
)

// State representa el estado alostático y de ciclo de vida de una tarea.
type State string

const (
	StateBacklog    State = "backlog"
	StateInProgress State = "in_progress"
	StatePaused     State = "paused"
	StateDone       State = "done"
	StateCancelled  State = "cancelled"
)

// WorkItem representa un ticket o elemento de trabajo en el tracker de Allostasis.
type WorkItem struct {
	ID              string    `json:"id"`
	Key             string    `json:"key"` // ej. OMNIFY-175, PROJ-10
	ProjectID       string    `json:"project_id,omitempty"`
	ProjectKey      string    `json:"project_key,omitempty"`
	SequenceID      int       `json:"sequence_id,omitempty"`
	Title           string    `json:"title"`
	Description     string    `json:"description,omitempty"`
	DescriptionHTML string    `json:"description_html,omitempty"`
	State           State     `json:"state"`
	StateName       string    `json:"state_name,omitempty"`
	StateGroup      string    `json:"state_group,omitempty"`
	Priority        string    `json:"priority"`
	Assignee        string    `json:"assignee,omitempty"`
	Assignees       []string  `json:"assignees,omitempty"`
	Labels          []string  `json:"labels,omitempty"`
	LabelsCSV       string    `json:"labels_csv,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// CreateIssueInput encapsula los datos requeridos para crear una tarea.
type CreateIssueInput struct {
	ProjectKey  string   `json:"project_key"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Priority    string   `json:"priority,omitempty"`
	StateName   string   `json:"state_name,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

// UpdateIssueInput encapsula las mutaciones permitidas sobre una tarea existente.
type UpdateIssueInput struct {
	Title       *string   `json:"title,omitempty"`
	Description *string   `json:"description,omitempty"`
	StateName   *string   `json:"state_name,omitempty"`
	Priority    *string   `json:"priority,omitempty"`
	Labels      *[]string `json:"labels,omitempty"`
}

// IssueFilter define criterios de consulta y paginación.
type IssueFilter struct {
	ProjectKey string
	State      string
	StateGroup string
	Assignee   string
	Query      string
	Limit      int
	Offset     int
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

// OutboxRecord representa una fila en la tabla outbox de despacho.
type OutboxRecord struct {
	ID          int64     `json:"id"`
	ClientID    string    `json:"client_id"`
	TicketRef   string    `json:"ticket_ref"`
	PayloadJSON string    `json:"payload_json"`
	State       string    `json:"state"`
	Attempts    int       `json:"attempts"`
	LastError   string    `json:"last_error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

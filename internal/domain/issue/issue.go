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

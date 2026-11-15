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
	ID          string    `json:"id"`
	Key         string    `json:"key"` // ej. OMNIFY-175, PROJ-10
	Title       string    `json:"title"`
	Description string    `json:"description"`
	State       State     `json:"state"`
	Priority    string    `json:"priority"`
	Assignee    string    `json:"assignee"`
	Labels      []string  `json:"labels"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
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

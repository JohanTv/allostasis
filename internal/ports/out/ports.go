package out

import (
	"context"

	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/domain/ledger"
	"github.com/allostasis/alo/internal/domain/workspace"
)

// IssueReader define el contrato atómico para consultas de solo lectura de tareas (ISP).
type IssueReader interface {
	GetIssue(ctx context.Context, key string) (*issue.WorkItem, error)
	ListIssues(ctx context.Context, state issue.State) ([]issue.WorkItem, error)
}

// IssueWriter define el contrato para mutaciones de tareas en la base local (ISP).
type IssueWriter interface {
	SaveIssue(ctx context.Context, item *issue.WorkItem) error
	EnqueueOutbox(ctx context.Context, event *issue.OutboxEvent) error
}

// WorklogReader define la lectura del ledger soberano y telemetría de intervalos.
type WorklogReader interface {
	GetWorklogs(ctx context.Context, filter ledger.CapacityBucket) ([]ledger.WorklogEntry, error)
	GetIntervals(ctx context.Context) ([]ledger.Interval, error)
}

// WorklogWriter define la inserción atómica de entradas en el ledger.
type WorklogWriter interface {
	SaveWorklog(ctx context.Context, entry *ledger.WorklogEntry) error
}

// WorkspaceExecutor orquesta la interacción transaccional con Git del host para worktrees.
type WorkspaceExecutor interface {
	CreateWorktree(ctx context.Context, c *workspace.Container) error
	AuditMerge(ctx context.Context, binding *workspace.WorktreeBinding, baseBranch string) (workspace.MergeStatus, error)
	PruneWorktree(ctx context.Context, c *workspace.Container, backupPath string) error
}

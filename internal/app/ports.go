package app

import (
	"context"

	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/domain/ledger"
	"github.com/allostasis/alo/internal/domain/workspace"
)

// IssueStore define el contrato para el almacenamiento y consulta de tareas y metadatos locales.
type IssueStore interface {
	GetIssue(ctx context.Context, key string) (*issue.WorkItem, error)
	ListIssues(ctx context.Context, state issue.State) ([]issue.WorkItem, error)
	ListIssuesFiltered(ctx context.Context, filter issue.IssueFilter) ([]issue.WorkItem, error)
	SearchIssues(ctx context.Context, query string, project string, limit int) ([]issue.WorkItem, error)
	SaveIssue(ctx context.Context, item *issue.WorkItem) error
	SaveIssueSnapshot(ctx context.Context, issueID, ticketID, issueJSON string) error
	DeleteIssue(ctx context.Context, ticketID string) error

	// Proyectos
	GetProject(ctx context.Context, identifier string) (*issue.ProjectRecord, error)
	ListProjects(ctx context.Context) ([]issue.ProjectRecord, error)
	SaveProject(ctx context.Context, p issue.ProjectRecord) error

	// Estados
	GetState(ctx context.Context, projectID, name string) (*issue.StateRecord, error)
	SaveState(ctx context.Context, st issue.StateRecord) error

	// Etiquetas
	ListLabels(ctx context.Context, projectID string) ([]issue.Label, error)
	SaveLabel(ctx context.Context, label issue.Label) error

	// Foco
	SetFocus(ctx context.Context, ticketID, sessionID string) (*issue.FocusRecord, error)
	ClearFocus(ctx context.Context, sessionID string) (*issue.FocusRecord, error)
	GetFocusStatus(ctx context.Context, sessionID string) (*issue.FocusRecord, error)

	// Enlaces y Adjuntos
	AddLink(ctx context.Context, link issue.Link) error
	ListLinks(ctx context.Context, ticketID string) ([]issue.Link, error)
	DeleteLink(ctx context.Context, linkID string) error

	AddAttachment(ctx context.Context, att issue.Attachment) error
	ListAttachments(ctx context.Context, ticketID string) ([]issue.Attachment, error)
	DeleteAttachment(ctx context.Context, attID string) error

	// Outbox
	EnqueueOutbox(ctx context.Context, clientID, ticketRef, payloadJSON, targetWS string) error
	ListOutbox(ctx context.Context, includeAll bool) ([]issue.OutboxRecord, error)
	DropOutboxItem(ctx context.Context, id int64) error
	UpdateOutboxSent(ctx context.Context, id int64, planeCommentID string) error
	UpdateOutboxFailed(ctx context.Context, id int64, errMsg string) error

	// Reportes
	GetTimeReport(ctx context.Context, fromDate, toDate, track, workerType string) (*issue.TimeReport, error)
	GetFlowReport(ctx context.Context, fromDate, toDate string) (*issue.FlowReport, error)
}

// WorklogStore define el contrato para el ledger soberano de tiempo e intervalos.
type WorklogStore interface {
	GetWorklogs(ctx context.Context, filter ledger.CapacityBucket) ([]ledger.WorklogEntry, error)
	GetIntervals(ctx context.Context) ([]ledger.Interval, error)
	SaveWorklog(ctx context.Context, entry *ledger.WorklogEntry) error
}

// StatusProvider resuelve el estado de ciclo de vida de un ticket para la auditoría de worktrees.
type StatusProvider = func(ticketID string) (stateGroup, stateName string)

// WorkspaceRunner define el contrato para interacción con Git del host.
type WorkspaceRunner interface {
	ResolveRepo(ctx context.Context, token string) (string, error)
	CreateWorktree(ctx context.Context, c *workspace.Container) error
	ListContainers(ctx context.Context, baseDir string) ([]workspace.Container, error)
	AuditContainers(ctx context.Context, baseDir, target, preferredBase string, noFetch bool, statusProvider StatusProvider) ([]workspace.ContainerAudit, error)
	PruneContainers(ctx context.Context, baseDir, target, preferredBase string, execute, includeStale, noFetch bool, backupDir string, statusProvider StatusProvider) (*workspace.PruneResult, error)
}

// RemoteTrackerClient define la interfaz para comunicación con el backend de tracking remoto.
type RemoteTrackerClient interface {
	Whoami(ctx context.Context) (*issue.UserInfo, error)
	FetchProjects(ctx context.Context) ([]issue.ProjectRecord, error)
	FetchStates(ctx context.Context, projectID string) ([]issue.StateRecord, error)
	FetchLabels(ctx context.Context, projectID string) ([]issue.Label, error)
	FetchIssues(ctx context.Context, projectID, projectKey string, limit int) ([]issue.IssueSnapshot, error)
	CreateIssueRemote(ctx context.Context, projectID string, input issue.CreateIssueInput, stateID string) (*issue.WorkItem, error)
	UpdateIssueRemote(ctx context.Context, projectID, issueID string, input issue.UpdateIssueInput, stateID string) error
	DeleteIssueRemote(ctx context.Context, projectID, issueID string) error
	SendCommentRaw(ctx context.Context, targetWS, projectID, issueID, payloadJSON string) (string, error)
	EditCommentRemote(ctx context.Context, projectID, issueID, commentID, newBody string) error
	DeleteCommentRemote(ctx context.Context, projectID, issueID, commentID string) error
	AddLinkRemote(ctx context.Context, projectID, issueID, title, linkURL string) (*issue.Link, error)
	ListLinksRemote(ctx context.Context, projectID, issueID string) ([]issue.Link, error)
	DeleteLinkRemote(ctx context.Context, projectID, issueID, linkID string) error
}

package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/allostasis/alo/internal/domain/workspace"
)

// WorkspaceService orquesta la creación, inspección, auditoría y poda de contenedores worktree multi-repo.
type WorkspaceService struct {
	runner     WorkspaceRunner
	issueStore IssueStore
}

// NewWorkspaceService crea una nueva instancia del servicio de worktrees desacoplado de infraestructura.
func NewWorkspaceService(runner WorkspaceRunner, issueStore IssueStore) *WorkspaceService {
	return &WorkspaceService{
		runner:     runner,
		issueStore: issueStore,
	}
}

// CreateContainerInput encapsula los parámetros para crear un contenedor transaccional.
type CreateContainerInput struct {
	TicketID   string
	Slug       string
	RepoTokens []string
	BranchType string
	BaseRef    string
}

// Create construye y monta un contenedor transaccional de worktrees para 1 o más repositorios.
func (s *WorkspaceService) Create(ctx context.Context, input CreateContainerInput) (*workspace.Container, error) {
	if s.runner == nil {
		return nil, fmt.Errorf("ejecutor de workspace no inicializado")
	}

	ticketID := strings.ToUpper(strings.TrimSpace(input.TicketID))
	if ticketID == "" {
		return nil, fmt.Errorf("ticket ID no puede estar vacío")
	}

	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		slug = strings.ToLower(ticketID)
	}

	repoTokens := input.RepoTokens
	if len(repoTokens) == 0 {
		repoTokens = []string{"."}
	}

	branchPrefix := input.BranchType
	if branchPrefix == "" {
		branchPrefix = "feat"
	}

	var bindings []workspace.WorktreeBinding
	for _, tok := range repoTokens {
		resolved, err := s.runner.ResolveRepo(ctx, tok)
		if err != nil {
			return nil, fmt.Errorf("no se pudo resolver el repositorio '%s': %w", tok, err)
		}
		repoName := filepath.Base(resolved)
		branchName := fmt.Sprintf("%s/%s", branchPrefix, slug)

		bindings = append(bindings, workspace.WorktreeBinding{
			RepoName: repoName,
			RepoPath: resolved,
			Branch:   branchName,
		})
	}

	containerPath := filepath.Join(".worktrees", slug)
	container := &workspace.Container{
		TicketID: ticketID,
		Slug:     slug,
		Path:     containerPath,
		BaseRef:  input.BaseRef,
		Bindings: bindings,
	}

	if err := s.runner.CreateWorktree(ctx, container); err != nil {
		return nil, fmt.Errorf("error al crear worktrees: %w", err)
	}

	return container, nil
}

// List inspecciona el directorio base y lista todos los contenedores activos.
func (s *WorkspaceService) List(ctx context.Context, baseDir string) ([]workspace.Container, error) {
	if s.runner == nil {
		return nil, fmt.Errorf("ejecutor de workspace no inicializado")
	}
	if baseDir == "" {
		baseDir = ".worktrees"
	}
	return s.runner.ListContainers(ctx, baseDir)
}

// Audit analiza el estado de los worktrees activos (merge status, ramas sucias, estado en tracker).
func (s *WorkspaceService) Audit(ctx context.Context, baseDir, target, preferredBase string, noFetch bool) ([]workspace.ContainerAudit, error) {
	if s.runner == nil {
		return nil, fmt.Errorf("ejecutor de workspace no inicializado")
	}
	if baseDir == "" {
		baseDir = ".worktrees"
	}

	statusProvider := func(ticketID string) (string, string) {
		if s.issueStore == nil {
			return "", ""
		}
		item, err := s.issueStore.GetIssue(ctx, ticketID)
		if err != nil || item == nil {
			return "", ""
		}
		return item.StateGroup, item.StateName
	}

	return s.runner.AuditContainers(ctx, baseDir, target, preferredBase, noFetch, statusProvider)
}

// Prune limpia o simula la desmantelación segura de contenedores de worktree.
func (s *WorkspaceService) Prune(ctx context.Context, baseDir, target, preferredBase string, execute, includeStale, noFetch bool, backupDir string) (*workspace.PruneResult, error) {
	if s.runner == nil {
		return nil, fmt.Errorf("ejecutor de workspace no inicializado")
	}
	if baseDir == "" {
		baseDir = ".worktrees"
	}

	statusProvider := func(ticketID string) (string, string) {
		if s.issueStore == nil {
			return "", ""
		}
		item, err := s.issueStore.GetIssue(ctx, ticketID)
		if err != nil || item == nil {
			return "", ""
		}
		return item.StateGroup, item.StateName
	}

	return s.runner.PruneContainers(ctx, baseDir, target, preferredBase, execute, includeStale, noFetch, backupDir, statusProvider)
}

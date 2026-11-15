package workspace

import "time"

// MergeStatus clasifica el estado de integración de una rama tras la auditoría con git merge-tree.
type MergeStatus string

const (
	StatusUnmerged     MergeStatus = "unmerged"      // Cambios pendientes sin integrar
	StatusMergedDirect MergeStatus = "merged_direct" // Integrado limpiamente vía merge commit o fast-forward
	StatusMergedSquash MergeStatus = "merged_squash" // Integrado vía squash-merge (árboles idénticos en memoria)
	StatusStale        MergeStatus = "stale"         // Rama desactualizada respecto a la base upstream
)

// WorktreeBinding asocia un repositorio Git concreto a una rama dentro del contenedor multi-repo.
type WorktreeBinding struct {
	RepoName   string      `json:"repo_name"`
	RepoPath   string      `json:"repo_path"`
	Branch     string      `json:"branch"`
	Worktree   string      `json:"worktree_path"`
	Status     MergeStatus `json:"status"`
	HeadCommit string      `json:"head_commit"`
	IsClean    bool        `json:"is_clean"`
}

// Container representa el entorno multi-repo aislado en disco para un ticket específico (.worktrees/<TICKET>-<slug>).
type Container struct {
	TicketID  string            `json:"ticket_id"`
	Slug      string            `json:"slug"`
	Path      string            `json:"path"`
	BaseRef   string            `json:"base_ref"` // ej. main, dev
	Bindings  []WorktreeBinding `json:"bindings"`
	CreatedAt time.Time         `json:"created_at"`
}

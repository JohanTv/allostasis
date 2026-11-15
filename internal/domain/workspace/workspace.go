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

// RepoAudit resume la auditoría de integración y limpieza de un repositorio individual.
type RepoAudit struct {
	Name           string `json:"name"`
	Path           string `json:"path"`
	Branch         string `json:"branch"`
	Commit         string `json:"commit"`
	BaseBranch     string `json:"base_branch"`
	BaseRef        string `json:"base_ref"`
	IsClean        bool   `json:"is_clean"`
	DirtyCount     int    `json:"dirty_count"`
	IsMerged       bool   `json:"is_merged"`
	MergeMethod    string `json:"merge_method,omitempty"`
	CommitsAhead   int    `json:"commits_ahead"`
	IsProtected    bool   `json:"is_protected"`
	StatusCategory string `json:"status_category"`
}

// ContainerCategory categoriza la salud y estado de ciclo de vida del contenedor de worktree.
type ContainerCategory string

const (
	CategorySeguroParaPodar     ContainerCategory = "SEGURO_PARA_PODAR"
	CategoryAbandonadoLimpio    ContainerCategory = "ABANDONADO_LIMPIO"
	CategoryAbandonadoCambios   ContainerCategory = "ABANDONADO_CON_CAMBIOS"
	CategoryMergeadoConDirty    ContainerCategory = "MERGEADO_CON_DIRTY"
	CategoryEnCurso             ContainerCategory = "EN_CURSO"
	CategoryParcialmenteMerge   ContainerCategory = "PARCIALMENTE_MERGEADO"
	CategoryBloqueadoPorCambios ContainerCategory = "BLOQUEADO_POR_CAMBIOS"
	CategoryProtegido           ContainerCategory = "PROTEGIDO"
)

// ContainerAudit contiene el diagnóstico integral de un contenedor de worktrees.
type ContainerAudit struct {
	Container       string            `json:"container"`
	TicketID        string            `json:"ticket_id,omitempty"`
	Path            string            `json:"path"`
	Category        ContainerCategory `json:"category"`
	Reason          string            `json:"reason"`
	PlaneStateGroup string            `json:"plane_state_group,omitempty"`
	PlaneStateName  string            `json:"plane_state_name,omitempty"`
	MultiRepo       bool              `json:"multi_repo"`
	Repos           []RepoAudit       `json:"repos"`
}

// BlockedContainer representa un contenedor que no puede podarse automáticamente.
type BlockedContainer struct {
	Container string            `json:"container"`
	Category  ContainerCategory `json:"category"`
	Reason    string            `json:"reason"`
}

// PruneResult encapsula el reporte de una operación de poda (dry-run o ejecución real).
type PruneResult struct {
	Mode         string             `json:"mode"`
	PrunedSafe   []string           `json:"pruned_safe"`
	PrunedStale  []string           `json:"pruned_stale"`
	SkippedStale []string           `json:"skipped_stale"`
	Blocked      []BlockedContainer `json:"blocked"`
	Backups      []string           `json:"backups,omitempty"`
}

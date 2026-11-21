package gitcli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JohanTv/allostasis/internal/adapters/gitcli"
	"github.com/JohanTv/allostasis/internal/domain/workspace"
)

// TestGitCLI_PathTraversal_And_FlagInjection evalúa la resistencia frente a inyecciones de flags
// y escapes de directorio (path traversal) en nombres de repositorios y ramas.
func TestGitCLI_PathTraversal_And_FlagInjection(t *testing.T) {
	tempBase := t.TempDir()
	mainRepoDir := filepath.Join(tempBase, "repo-core")
	if err := os.MkdirAll(mainRepoDir, 0755); err != nil {
		t.Fatalf("error creando repo: %v", err)
	}
	setupTestGitRepo(t, mainRepoDir)

	runner := gitcli.NewRunner(5 * time.Second)
	executor := gitcli.NewGitExecutor(runner)
	ctx := context.Background()

	t.Run("RepoName_PathTraversal_BreaksOutOfContainer", func(t *testing.T) {
		containerDir := filepath.Join(tempBase, ".worktrees", "TICKET-1-traversal")
		escapedTargetDir := filepath.Join(tempBase, "escaped_repo")

		// RepoName malicioso con path traversal '../escaped_repo'
		relTraversal := filepath.Join("..", "..", "escaped_repo")

		container := &workspace.Container{
			TicketID: "TICKET-1",
			Path:     containerDir,
			Bindings: []workspace.WorktreeBinding{
				{
					RepoName: relTraversal,
					RepoPath: mainRepoDir,
					Branch:   "feat/traversal-test",
				},
			},
		}

		err := executor.CreateWorktree(ctx, container)
		if err == nil {
			// Si no hubo validación de path traversal, el worktree se creó fuera de containerDir
			t.Logf("AUDITORÍA DE VULNERABILIDAD: CreateWorktree permitió RepoName con path traversal (%s). Worktree creado en: %s", relTraversal, container.Bindings[0].Worktree)

			// Verificar si escapó fuera de containerDir
			if !strings.HasPrefix(container.Bindings[0].Worktree, containerDir) {
				t.Errorf("FALLO DE SEGURIDAD: Path Traversal verificado. El worktree '%s' se ubicó fuera del contenedor '%s'",
					container.Bindings[0].Worktree, containerDir)
			}

			// Limpieza manual
			_ = os.RemoveAll(escapedTargetDir)
			_ = os.RemoveAll(containerDir)
		} else {
			t.Logf("CreateWorktree rechazó o falló ante path traversal: %v", err)
		}
	})

	t.Run("FlagInjection_In_BranchName", func(t *testing.T) {
		containerDir := filepath.Join(tempBase, ".worktrees", "TICKET-2-flag-branch")
		container := &workspace.Container{
			TicketID: "TICKET-2",
			Path:     containerDir,
			Bindings: []workspace.WorktreeBinding{
				{
					RepoName: "repo-core",
					RepoPath: mainRepoDir,
					Branch:   "--orphan", // Inyección de flag git
				},
			},
		}

		err := executor.CreateWorktree(ctx, container)
		if err != nil {
			t.Logf("Branch con inyección de flag '--orphan' interceptado/fallado por Git: %v", err)
		} else {
			t.Logf("AVISO: Branch '--orphan' aceptado como nombre de rama por Git CLI")
			_ = executor.PruneWorktree(ctx, container, "")
		}
	})

	t.Run("FlagInjection_In_BaseRef", func(t *testing.T) {
		containerDir := filepath.Join(tempBase, ".worktrees", "TICKET-3-flag-baseref")
		container := &workspace.Container{
			TicketID: "TICKET-3",
			Path:     containerDir,
			BaseRef:  "--lock", // Inyección de flag que bloquea el worktree
			Bindings: []workspace.WorktreeBinding{
				{
					RepoName: "repo-core",
					RepoPath: mainRepoDir,
					Branch:   "feat/normal-branch",
				},
			},
		}

		err := executor.CreateWorktree(ctx, container)
		if err != nil {
			t.Logf("BaseRef con inyección de flag '--lock' provocó error: %v", err)
		} else {
			t.Logf("AVISO: BaseRef '--lock' fue inyectado en los argumentos de git worktree add!")
			_ = executor.PruneWorktree(ctx, container, "")
		}
	})
}

// TestGitCLI_RollbackResilience_MultiRepo simula un fallo a mitad de la creación
// de un contenedor multi-repo (repo 1 ok, repo 2 falla).
// Audita que:
// 1. El directorio contenedor sea eliminado.
// 2. El worktree de repo 1 sea desvinculado limpiamente de git (sin dejar worktrees zombies / prunables).
// 3. No queden ramas huérfanas en repo 1 que impidan futuros intentos.
func TestGitCLI_RollbackResilience_MultiRepo(t *testing.T) {
	tempBase := t.TempDir()

	// Repo 1 válido
	repo1Dir := filepath.Join(tempBase, "repo-alpha")
	if err := os.MkdirAll(repo1Dir, 0755); err != nil {
		t.Fatalf("error creando repo 1: %v", err)
	}
	setupTestGitRepo(t, repo1Dir)

	// Repo 2 deliberadamente corrupto / no existente para forzar fallo a mitad de proceso
	repo2Dir := filepath.Join(tempBase, "repo-beta-invalid")

	runner := gitcli.NewRunner(5 * time.Second)
	executor := gitcli.NewGitExecutor(runner)
	ctx := context.Background()

	worktreeRoot := filepath.Join(tempBase, ".worktrees", "ALO-500-multi-fail")
	branchName := "feat/ALO-500-test-rollback"

	container := &workspace.Container{
		TicketID: "ALO-500",
		Path:     worktreeRoot,
		Bindings: []workspace.WorktreeBinding{
			{
				RepoName: "repo-alpha",
				RepoPath: repo1Dir,
				Branch:   branchName,
			},
			{
				RepoName: "repo-beta",
				RepoPath: repo2Dir, // Ruta inválida: forzará error en b.RepoPath
				Branch:   branchName,
			},
		},
	}

	err := executor.CreateWorktree(ctx, container)
	if err == nil {
		t.Fatal("CreateWorktree debió fallar debido a repo 2 inexistente")
	}

	t.Logf("CreateWorktree falló como se esperaba: %v", err)

	// 1. Verificar si la carpeta contenedora fue removida
	if _, statErr := os.Stat(worktreeRoot); !os.IsNotExist(statErr) {
		t.Errorf("FALLO ROLLBACK: El directorio contenedor '%s' no fue eliminado tras el rollback", worktreeRoot)
	}

	// 2. Verificar estado de git worktree en repo 1
	// Si el rollback falló al desvincular (por ejecutar git worktree remove en Dir=""),
	// repo 1 tendrá un worktree zombie huérfano.
	out, _, wtListErr := runner.Run(ctx, repo1Dir, "worktree", "list", "--porcelain")
	if wtListErr != nil {
		t.Fatalf("error al consultar worktree list en repo1: %v", wtListErr)
	}

	t.Logf("git worktree list en repo-alpha tras rollback:\n%s", out)
	if strings.Contains(out, filepath.Join(worktreeRoot, "repo-alpha")) {
		t.Errorf("FALLO RESILIENCIA: Repo 1 mantiene un worktree zombi registrado en .git tras el rollback fallido!")
	}

	// 3. Verificar si la rama creada con -b quedó huérfana en repo 1
	branchOut, _, _ := runner.Run(ctx, repo1Dir, "branch", "--list", branchName)
	if strings.TrimSpace(branchOut) != "" {
		t.Logf("AVISO AUDITORÍA: La rama '%s' creada en el primer repositorio no fue eliminada por el rollback", branchName)
	}
}

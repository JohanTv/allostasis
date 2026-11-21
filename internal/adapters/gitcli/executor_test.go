package gitcli_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JohanTv/allostasis/internal/adapters/gitcli"
	"github.com/JohanTv/allostasis/internal/domain/workspace"
)

func setupTestGitRepo(t *testing.T, dir string) {
	runner := gitcli.NewRunner(10 * time.Second)
	ctx := context.Background()

	// git init -b main
	if _, _, err := runner.Run(ctx, dir, "init", "-b", "main"); err != nil {
		t.Fatalf("fallo al inicializar repo git: %v", err)
	}

	// git config user.name / email
	_, _, _ = runner.Run(ctx, dir, "config", "user.name", "Test Runner")
	_, _, _ = runner.Run(ctx, dir, "config", "user.email", "test@allostasis.local")
	_, _, _ = runner.Run(ctx, dir, "config", "commit.gpgsign", "false")

	// Commit inicial
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# Repo Principal\n"), 0644); err != nil {
		t.Fatalf("error escribiendo README: %v", err)
	}

	if _, _, err := runner.Run(ctx, dir, "add", "README.md"); err != nil {
		t.Fatalf("error al git add: %v", err)
	}
	if _, _, err := runner.Run(ctx, dir, "commit", "-m", "chore: initial commit"); err != nil {
		t.Fatalf("error al commit inicial: %v", err)
	}
}

func TestGitExecutor_LifecycleIntegration(t *testing.T) {
	tempBase := t.TempDir()
	mainRepoDir := filepath.Join(tempBase, "repo-core")
	if err := os.MkdirAll(mainRepoDir, 0755); err != nil {
		t.Fatalf("error creando directorio: %v", err)
	}

	setupTestGitRepo(t, mainRepoDir)

	runner := gitcli.NewRunner(10 * time.Second)
	executor := gitcli.NewGitExecutor(runner)
	ctx := context.Background()

	// 1. Probar ResolveRepo
	resolved, err := executor.ResolveRepo(ctx, mainRepoDir)
	if err != nil {
		t.Fatalf("ResolveRepo falló: %v", err)
	}
	if resolved != mainRepoDir {
		t.Errorf("ResolveRepo esperado %s, obtenido %s", mainRepoDir, resolved)
	}

	// 2. Probar CreateWorktree
	worktreeRoot := filepath.Join(tempBase, ".worktrees", "ALO-101-test-flow")
	container := &workspace.Container{
		TicketID: "ALO-101",
		Slug:     "test-flow",
		Path:     worktreeRoot,
		BaseRef:  "main",
		Bindings: []workspace.WorktreeBinding{
			{
				RepoName: "repo-core",
				RepoPath: mainRepoDir,
				Branch:   "feat/ALO-101-test-flow",
			},
		},
	}

	if err := executor.CreateWorktree(ctx, container); err != nil {
		t.Fatalf("CreateWorktree falló: %v", err)
	}

	// Verificar que el directorio worktree existe
	wtPath := filepath.Join(worktreeRoot, "repo-core")
	if fi, err := os.Stat(wtPath); err != nil || !fi.IsDir() {
		t.Fatalf("el directorio del worktree no fue creado en: %s", wtPath)
	}

	// 3. Probar ListContainers
	containers, err := executor.ListContainers(ctx, filepath.Join(tempBase, ".worktrees"))
	if err != nil {
		t.Fatalf("ListContainers falló: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("se esperaba 1 contenedor, obtenidos %d", len(containers))
	}
	if len(containers[0].Bindings) != 1 {
		t.Fatalf("se esperaba 1 binding, obtenidos %d", len(containers[0].Bindings))
	}

	// 4. Probar AuditMerge (inicialmente sin mergear)
	status, err := executor.AuditMerge(ctx, &container.Bindings[0], "main")
	if err != nil {
		t.Fatalf("AuditMerge falló: %v", err)
	}
	if status != workspace.StatusMergedDirect {
		// Al ser creada sobre main sin nuevos commits, merge-base coincide con branch commit
		t.Logf("AuditMerge status inicial: %s", status)
	}

	// Crear un nuevo commit en el worktree
	featureFile := filepath.Join(wtPath, "feature.txt")
	_ = os.WriteFile(featureFile, []byte("feature content\n"), 0644)
	_, _, _ = runner.Run(ctx, wtPath, "add", "feature.txt")
	_, _, _ = runner.Run(ctx, wtPath, "commit", "-m", "feat: implement test feature")

	// Ahora no debe estar mergeada en main
	statusAfterCommit, err := executor.AuditMerge(ctx, &container.Bindings[0], "main")
	if err != nil {
		t.Fatalf("AuditMerge después de commit falló: %v", err)
	}
	if statusAfterCommit != workspace.StatusUnmerged {
		t.Errorf("se esperaba StatusUnmerged, obtenido: %s", statusAfterCommit)
	}

	// 5. Probar PruneWorktree
	backupDir := filepath.Join(tempBase, "backups")
	if err := executor.PruneWorktree(ctx, container, backupDir); err != nil {
		t.Fatalf("PruneWorktree falló: %v", err)
	}

	// Verificar que el directorio de contenedor fue removido
	if _, err := os.Stat(worktreeRoot); !os.IsNotExist(err) {
		t.Errorf("el directorio contenedor aún existe tras Prune: %s", worktreeRoot)
	}
}

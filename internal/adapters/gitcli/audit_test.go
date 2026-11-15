package gitcli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/allostasis/alo/internal/domain/workspace"
)

func TestAuditContainers_EmptyDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "TestAuditContainers_EmptyDir*")
	if err != nil {
		t.Fatalf("fallo al crear directorio temporal: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	runner := NewRunner(10 * time.Second)
	executor := NewGitExecutor(runner)

	audited, err := executor.AuditContainers(context.Background(), tmpDir, "", "", true, nil)
	if err != nil {
		t.Fatalf("AuditContainers falló inesperadamente: %v", err)
	}
	if len(audited) != 0 {
		t.Errorf("se esperaban 0 contenedores, obtenidos: %d", len(audited))
	}
}

func TestAuditContainers_LifecycleAndPrune(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "TestAuditContainers_Lifecycle*")
	if err != nil {
		t.Fatalf("fallo al crear directorio temporal: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	runner := NewRunner(10 * time.Second)
	executor := NewGitExecutor(runner)
	ctx := context.Background()

	// 1. Crear un repo base 'origin-repo'
	baseRepo := filepath.Join(tmpDir, "origin-repo")
	_ = os.MkdirAll(baseRepo, 0755)
	_, _, _ = runner.Run(ctx, baseRepo, "init", "-b", "main")
	_, _, _ = runner.Run(ctx, baseRepo, "config", "user.name", "TestUser")
	_, _, _ = runner.Run(ctx, baseRepo, "config", "user.email", "test@example.com")
	_ = os.WriteFile(filepath.Join(baseRepo, "README.md"), []byte("# Base"), 0644)
	_, _, _ = runner.Run(ctx, baseRepo, "add", "README.md")
	_, _, _ = runner.Run(ctx, baseRepo, "commit", "-m", "initial commit")

	// 2. Crear un worktree en .worktrees/ALO-100-test
	wtRoot := filepath.Join(tmpDir, ".worktrees")
	containerPath := filepath.Join(wtRoot, "alo-100-test")
	_ = os.MkdirAll(containerPath, 0755)

	container := &workspace.Container{
		TicketID: "ALO-100",
		Slug:     "alo-100-test",
		Path:     containerPath,
		BaseRef:  "main",
		Bindings: []workspace.WorktreeBinding{
			{
				RepoName: "origin-repo",
				RepoPath: baseRepo,
				Branch:   "feat/alo-100-test",
			},
		},
	}

	if err := executor.CreateWorktree(ctx, container); err != nil {
		t.Fatalf("CreateWorktree falló: %v", err)
	}

	// 2b. Crear un commit no mergeado en el worktree
	wtRepoDir := filepath.Join(containerPath, "origin-repo")
	_ = os.WriteFile(filepath.Join(wtRepoDir, "work.txt"), []byte("trabajo en progreso"), 0644)
	_, _, _ = runner.Run(ctx, wtRepoDir, "add", "work.txt")
	_, _, _ = runner.Run(ctx, wtRepoDir, "commit", "-m", "feat: work in progress")

	// 3. Auditar inicialmente (rama no mergeada y limpia)
	audited, err := executor.AuditContainers(ctx, wtRoot, "", "main", true, nil)
	if err != nil {
		t.Fatalf("AuditContainers falló: %v", err)
	}
	if len(audited) != 1 {
		t.Fatalf("se esperaba 1 contenedor auditado, obtenidos %d", len(audited))
	}
	if audited[0].Category != workspace.CategoryEnCurso {
		t.Errorf("se esperaba categoría EN_CURSO para rama con commits sin mergear, obtenida: %s", audited[0].Category)
	}

	// 4. Test Prune Dry-Run (bloqueado porque está EN_CURSO)
	pruneDry, err := executor.PruneContainers(ctx, wtRoot, "ALO-100", "", false, false, true, "", nil)
	if err != nil {
		t.Fatalf("PruneContainers dry-run falló: %v", err)
	}
	if pruneDry.Mode != "dry-run" {
		t.Errorf("se esperaba mode dry-run, obtenido: %s", pruneDry.Mode)
	}
	if len(pruneDry.Blocked) != 1 {
		t.Errorf("se esperaba 1 contenedor bloqueado, obtenidos: %d", len(pruneDry.Blocked))
	}

	// 5. Merge de la rama a main en baseRepo
	_, _, _ = runner.Run(ctx, baseRepo, "merge", "feat/alo-100-test")

	// 6. Ahora auditar de nuevo: debe ser SEGURO_PARA_PODAR
	auditedMerged, err := executor.AuditContainers(ctx, wtRoot, "", "main", true, nil)
	if err != nil {
		t.Fatalf("AuditContainers post-merge falló: %v", err)
	}
	if len(auditedMerged) != 1 || auditedMerged[0].Category != workspace.CategorySeguroParaPodar {
		t.Errorf("se esperaba categoría SEGURO_PARA_PODAR, obtenida: %s", auditedMerged[0].Category)
	}

	// 7. Prune Execute real
	pruneExec, err := executor.PruneContainers(ctx, wtRoot, "ALO-100", "", true, false, true, "", nil)
	if err != nil {
		t.Fatalf("PruneContainers execute falló: %v", err)
	}
	if len(pruneExec.PrunedSafe) != 1 {
		t.Errorf("se esperaba 1 contenedor podado seguro, obtenidos: %d", len(pruneExec.PrunedSafe))
	}

	// Verificar que el directorio se eliminó
	if _, err := os.Stat(containerPath); !os.IsNotExist(err) {
		t.Errorf("el contenedor '%s' no debería existir tras poda execute", containerPath)
	}
}

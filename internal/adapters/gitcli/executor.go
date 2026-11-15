package gitcli

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/allostasis/alo/internal/domain/workspace"
)

// GitExecutor implementa WorkspaceExecutor y operaciones sobre repositorios Git.
type GitExecutor struct {
	runner *Runner
}

// NewGitExecutor crea un nuevo adaptador GitExecutor.
func NewGitExecutor(runner *Runner) *GitExecutor {
	if runner == nil {
		runner = NewRunner(30 * time.Second)
	}
	return &GitExecutor{runner: runner}
}

// ResolveRepo resuelve un token (ruta o nombre relativo) a la raíz del repositorio Git.
func (e *GitExecutor) ResolveRepo(ctx context.Context, token string) (string, error) {
	// 1. Si es ruta directa existente
	if fi, err := os.Stat(token); err == nil && fi.IsDir() {
		out, _, err := e.runner.Run(ctx, token, "rev-parse", "--show-toplevel")
		if err == nil {
			return filepath.Clean(out), nil
		}
		return "", fmt.Errorf("la ruta '%s' existe pero no es un repositorio Git válido", token)
	}

	// 2. Si es nombre relativo en el directorio actual o superior
	base := filepath.Base(token)
	if base == token {
		for _, cand := range []string{token, filepath.Join("..", token)} {
			if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
				out, _, err := e.runner.Run(ctx, cand, "rev-parse", "--show-toplevel")
				if err == nil {
					return filepath.Clean(out), nil
				}
			}
		}
	}

	return "", fmt.Errorf("repositorio no encontrado para token '%s'", token)
}

// CreateWorktree crea el contenedor y agrega cada worktree atómicamente con rollback en caso de fallo.
func (e *GitExecutor) CreateWorktree(ctx context.Context, c *workspace.Container) error {
	if len(c.Bindings) == 0 {
		return fmt.Errorf("no hay repositorios asignados al contenedor %s", c.TicketID)
	}

	absPath, err := filepath.Abs(c.Path)
	if err != nil {
		return fmt.Errorf("error obteniendo ruta absoluta de '%s': %w", c.Path, err)
	}
	c.Path = absPath

	// Anti Flag Injection en BaseRef
	if c.BaseRef != "" && strings.HasPrefix(strings.TrimSpace(c.BaseRef), "-") {
		return fmt.Errorf("referencia base inválida (no puede comenzar con guion): %s", c.BaseRef)
	}

	// Pre-validación estricta de todos los bindings antes de mutar el sistema de archivos
	for _, b := range c.Bindings {
		cleanName := filepath.Base(b.RepoName)
		if cleanName == "." || cleanName == "/" || cleanName == ".." || cleanName != b.RepoName {
			return fmt.Errorf("intento de path traversal detectado en nombre de repositorio: %s", b.RepoName)
		}
		if strings.HasPrefix(strings.TrimSpace(b.Branch), "-") {
			return fmt.Errorf("nombre de rama inválido (no puede comenzar con guion): %s", b.Branch)
		}
	}

	// Crear el directorio contenedor (.worktrees/<TICKET>-<slug>)
	if err := os.MkdirAll(c.Path, 0755); err != nil {
		return fmt.Errorf("error al crear directorio contenedor '%s': %w", c.Path, err)
	}

	type createdWorktreeItem struct {
		repoPath string
		wtPath   string
		branch   string
	}
	createdItems := make([]createdWorktreeItem, 0, len(c.Bindings))

	// Rollback handler transaccional de alta precisión
	rollback := func() {
		for _, item := range createdItems {
			_, _, _ = e.runner.Run(ctx, item.repoPath, "worktree", "remove", "--force", item.wtPath)
			_, _, _ = e.runner.Run(ctx, item.repoPath, "branch", "-D", item.branch)
		}
		_ = os.RemoveAll(c.Path)
	}

	for i := range c.Bindings {
		b := &c.Bindings[i]
		cleanName := filepath.Base(b.RepoName)
		wtPath := filepath.Join(c.Path, cleanName)

		// Verificación de contención de path
		rel, err := filepath.Rel(c.Path, wtPath)
		if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
			rollback()
			return fmt.Errorf("violación de confinamiento de contenedor en '%s'", wtPath)
		}

		b.Worktree = wtPath

		// git worktree add -b <branch> <path> -- <baseRef>
		args := []string{"worktree", "add", "-b", b.Branch, wtPath}
		if c.BaseRef != "" {
			args = append(args, "--", c.BaseRef)
		}

		_, _, err = e.runner.Run(ctx, b.RepoPath, args...)
		if err != nil {
			// Intentar sin -b si la rama ya existía previamente
			argsRetry := []string{"worktree", "add", wtPath, b.Branch}
			_, _, errRetry := e.runner.Run(ctx, b.RepoPath, argsRetry...)
			if errRetry != nil {
				rollback()
				return fmt.Errorf("fallo al crear worktree en '%s' (rama: %s): %w", b.RepoName, b.Branch, err)
			}
		}

		createdItems = append(createdItems, createdWorktreeItem{
			repoPath: b.RepoPath,
			wtPath:   wtPath,
			branch:   b.Branch,
		})
		b.IsClean = true
		b.Status = workspace.StatusUnmerged
	}

	return nil
}

// AuditMerge determina si la rama del binding ha sido integrada en baseBranch.
// Soporta detección estándar y detección de squash-merge usando `git merge-tree --write-tree`.
func (e *GitExecutor) AuditMerge(ctx context.Context, binding *workspace.WorktreeBinding, baseBranch string) (workspace.MergeStatus, error) {
	if baseBranch == "" {
		baseBranch = "main"
	}

	repoDir := binding.Worktree
	if repoDir == "" {
		repoDir = binding.RepoPath
	}

	// 1. Obtener merge-base entre la rama y baseBranch
	mergeBase, _, err := e.runner.Run(ctx, repoDir, "merge-base", baseBranch, binding.Branch)
	if err != nil {
		return workspace.StatusUnmerged, fmt.Errorf("error al calcular merge-base: %w", err)
	}

	// 2. Obtener commit SHA de la rama
	branchCommit, _, err := e.runner.Run(ctx, repoDir, "rev-parse", binding.Branch)
	if err != nil {
		return workspace.StatusUnmerged, fmt.Errorf("error al obtener commit de rama '%s': %w", binding.Branch, err)
	}

	// Si merge-base == branchCommit, la rama está directamente mergeada (fast-forward o normal merge)
	if mergeBase == branchCommit {
		return workspace.StatusMergedDirect, nil
	}

	// 3. Detección de Squash-Merge con git merge-tree --write-tree
	// Simula el merge de la rama en baseBranch en memoria.
	mergeTreeOut, _, err := e.runner.Run(ctx, repoDir, "merge-tree", "--write-tree", baseBranch, binding.Branch)
	if err == nil {
		treeSHA := strings.TrimSpace(mergeTreeOut)
		// Obtener el tree SHA de baseBranch
		baseTreeSHA, _, err := e.runner.Run(ctx, repoDir, "rev-parse", fmt.Sprintf("%s^{tree}", baseBranch))
		if err == nil && treeSHA == strings.TrimSpace(baseTreeSHA) {
			// El resultado del merge no aporta diferencias => ya está integrado (squash merge)
			return workspace.StatusMergedSquash, nil
		}
	}

	// 4. Verificar si tiene cambios sin commitear en el worktree
	statusOut, _, err := e.runner.Run(ctx, repoDir, "status", "--porcelain")
	if err == nil && strings.TrimSpace(statusOut) == "" {
		binding.IsClean = true
	} else {
		binding.IsClean = false
	}

	return workspace.StatusUnmerged, nil
}

// PruneWorktree desmantela un contenedor, archivando cambios sucios si los hay.
func (e *GitExecutor) PruneWorktree(ctx context.Context, c *workspace.Container, backupDir string) error {
	for _, b := range c.Bindings {
		wtDir := b.Worktree
		if wtDir == "" {
			wtDir = filepath.Join(c.Path, b.RepoName)
		}

		// Comprobar si existen cambios no commiteados
		status, _, _ := e.runner.Run(ctx, wtDir, "status", "--porcelain")
		if strings.TrimSpace(status) != "" && backupDir != "" {
			_ = os.MkdirAll(backupDir, 0755)
			timestamp := time.Now().Format("20060102-150405")
			tarGzPath := filepath.Join(backupDir, fmt.Sprintf("%s-%s-%s.tar.gz", c.TicketID, b.RepoName, timestamp))
			_ = archiveDir(wtDir, tarGzPath)
		}

		// Desvincular de git
		_, _, _ = e.runner.Run(ctx, b.RepoPath, "worktree", "remove", "--force", wtDir)
	}

	// Eliminar directorio contenedor
	if err := os.RemoveAll(c.Path); err != nil {
		return fmt.Errorf("error al eliminar directorio de contenedor '%s': %w", c.Path, err)
	}

	return nil
}

// ListContainers inspecciona los worktrees activos en una raíz contenedora (ej. .worktrees/).
func (e *GitExecutor) ListContainers(ctx context.Context, rootDir string) ([]workspace.Container, error) {
	if _, err := os.Stat(rootDir); os.IsNotExist(err) {
		return []workspace.Container{}, nil
	}

	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, fmt.Errorf("error al leer directorio '%s': %w", rootDir, err)
	}

	var containers []workspace.Container
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		cPath := filepath.Join(rootDir, entry.Name())
		subEntries, err := os.ReadDir(cPath)
		if err != nil {
			continue
		}

		var bindings []workspace.WorktreeBinding
		for _, sub := range subEntries {
			if !sub.IsDir() {
				continue
			}
			subPath := filepath.Join(cPath, sub.Name())
			// Verificar si es un worktree válido inspeccionando .git
			gitPath := filepath.Join(subPath, ".git")
			if _, err := os.Stat(gitPath); err == nil {
				branch, _, _ := e.runner.Run(ctx, subPath, "branch", "--show-current")
				head, _, _ := e.runner.Run(ctx, subPath, "rev-parse", "--short", "HEAD")
				statusOut, _, _ := e.runner.Run(ctx, subPath, "status", "--porcelain")
				commonDir, _, _ := e.runner.Run(ctx, subPath, "rev-parse", "--path-format=absolute", "--git-common-dir")

				originalRepo := ""
				if strings.TrimSpace(commonDir) != "" {
					originalRepo = filepath.Dir(strings.TrimSpace(commonDir))
				}

				bindings = append(bindings, workspace.WorktreeBinding{
					RepoName:   sub.Name(),
					RepoPath:   originalRepo,
					Worktree:   subPath,
					Branch:     strings.TrimSpace(branch),
					HeadCommit: strings.TrimSpace(head),
					IsClean:    strings.TrimSpace(statusOut) == "",
				})
			}
		}

		if len(bindings) > 0 {
			containers = append(containers, workspace.Container{
				Slug:     entry.Name(),
				Path:     cPath,
				Bindings: bindings,
			})
		}
	}

	return containers, nil
}

// archiveDir empaqueta el contenido de un directorio en un archivo tar.gz
func archiveDir(srcDir, tarGzFile string) error {
	out, err := os.Create(tarGzFile)
	if err != nil {
		return err
	}
	defer out.Close()

	gw := gzip.NewWriter(out)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if relPath == "." || strings.HasPrefix(relPath, ".git") {
			return nil
		}

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(tw, file)
		return err
	})
}

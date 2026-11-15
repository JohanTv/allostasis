package gitcli

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/allostasis/alo/internal/domain/workspace"
)

var (
	ticketRegex       = regexp.MustCompile(`(?i)\b([A-Za-z0-9_]+-\d+)\b`)
	protectedBranches = map[string]bool{
		"main":    true,
		"master":  true,
		"dev":     true,
		"staging": true,
	}
)

func extractTicketID(text string) string {
	if text == "" {
		return ""
	}
	match := ticketRegex.FindStringSubmatch(text)
	if len(match) > 1 {
		return strings.ToUpper(match[1])
	}
	return ""
}

// checkGitMerged evalúa si una rama está integrada en la rama base:
// 1. Ancestro directo (merge normal o fast-forward).
// 2. Equivalencia de árbol (squash-merge en memoria con git merge-tree --write-tree).
func (e *GitExecutor) checkGitMerged(ctx context.Context, repoDir, branch, baseBranch string) (bool, string, string) {
	if baseBranch == "" {
		return false, "no_base", ""
	}

	baseRef := "origin/" + baseBranch
	_, _, err := e.runner.Run(ctx, repoDir, "rev-parse", "--verify", "--quiet", "refs/remotes/"+baseRef)
	if err != nil {
		baseRef = baseBranch
		_, _, err = e.runner.Run(ctx, repoDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+baseRef)
		if err != nil {
			return false, "base_not_found", ""
		}
	}

	// 1. Ancestro directo
	_, _, err = e.runner.Run(ctx, repoDir, "merge-base", "--is-ancestor", "HEAD", baseRef)
	if err == nil {
		return true, "direct", baseRef
	}

	// 2. Equivalencia Squash / Rebase vía merge-tree trifásico en memoria
	mtOut, _, err := e.runner.Run(ctx, repoDir, "merge-tree", "--write-tree", baseRef, "HEAD")
	if err == nil && strings.TrimSpace(mtOut) != "" {
		mergeTree := strings.TrimSpace(mtOut)
		btOut, _, err := e.runner.Run(ctx, repoDir, "rev-parse", fmt.Sprintf("%s^{tree}", baseRef))
		if err == nil && mergeTree == strings.TrimSpace(btOut) {
			return true, "squash", baseRef
		}
	}

	return false, "unmerged", baseRef
}

// AuditRepoState realiza una auditoría profunda del estado Git de un repositorio individual dentro de un worktree.
func (e *GitExecutor) AuditRepoState(ctx context.Context, repoDir, preferredBase string) (*workspace.RepoAudit, error) {
	root, _, err := e.runner.Run(ctx, repoDir, "rev-parse", "--show-toplevel")
	if err != nil || filepath.Clean(root) != filepath.Clean(repoDir) {
		return nil, fmt.Errorf("directorio '%s' no es la raíz de un repositorio Git válido", repoDir)
	}

	branch, _, err := e.runner.Run(ctx, repoDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch == "" {
		branch = "unknown"
	}

	commitLine, _, err := e.runner.Run(ctx, repoDir, "log", "-1", "--format=%h '%s'")
	if err != nil || commitLine == "" {
		commitLine = "(sin commits)"
	}

	statusOut, _, _ := e.runner.Run(ctx, repoDir, "status", "--porcelain")
	var dirtyLines []string
	for _, l := range strings.Split(statusOut, "\n") {
		if strings.TrimSpace(l) != "" {
			dirtyLines = append(dirtyLines, l)
		}
	}
	isClean := len(dirtyLines) == 0

	candidates := []string{"dev", "staging", "main", "master"}
	if preferredBase != "" {
		candidates = append([]string{preferredBase}, candidates...)
	}

	var foundBaseBranch string
	var foundBaseRef string
	isMerged := false
	mergeMethod := ""

	for _, cand := range candidates {
		if cand == "" {
			continue
		}
		merged, method, bRef := e.checkGitMerged(ctx, repoDir, branch, cand)
		if bRef != "" && foundBaseRef == "" {
			foundBaseBranch = cand
			foundBaseRef = bRef
		}
		if merged {
			isMerged = true
			mergeMethod = method
			foundBaseBranch = cand
			foundBaseRef = bRef
			break
		}
	}

	commitsAhead := 0
	if foundBaseRef != "" {
		countOut, _, err := e.runner.Run(ctx, repoDir, "rev-list", "--count", fmt.Sprintf("%s..HEAD", foundBaseRef))
		if err == nil {
			commitsAhead, _ = strconv.Atoi(strings.TrimSpace(countOut))
		}
	}

	isProtected := protectedBranches[branch]
	var statusCategory string
	if isProtected {
		statusCategory = "PROTECTED"
	} else if isClean && isMerged {
		statusCategory = "SAFE_TO_PRUNE"
	} else if !isClean && isMerged {
		statusCategory = "MERGED_DIRTY"
	} else if isClean && !isMerged {
		statusCategory = "UNMERGED_CLEAN"
	} else {
		statusCategory = "UNMERGED_DIRTY"
	}

	return &workspace.RepoAudit{
		Name:           filepath.Base(repoDir),
		Path:           repoDir,
		Branch:         branch,
		Commit:         commitLine,
		BaseBranch:     foundBaseBranch,
		BaseRef:        foundBaseRef,
		IsClean:        isClean,
		DirtyCount:     len(dirtyLines),
		IsMerged:       isMerged,
		MergeMethod:    mergeMethod,
		CommitsAhead:   commitsAhead,
		IsProtected:    isProtected,
		StatusCategory: statusCategory,
	}, nil
}

// StatusProvider es una función inyectable que provee (stateGroup, stateName) para un ticket sin acoplamiento a base de datos.
type StatusProvider = func(ticketID string) (stateGroup, stateName string)

// AuditContainer audita un contenedor (.worktrees/<TICKET>-<slug>) clasificando su salud e idoneidad para poda.
func (e *GitExecutor) AuditContainer(ctx context.Context, containerPath, preferredBase string, statusProvider StatusProvider) (*workspace.ContainerAudit, error) {
	cname := filepath.Base(containerPath)
	ticketID := extractTicketID(cname)

	var repos []workspace.RepoAudit
	multiRepo := false

	directRepo, err := e.AuditRepoState(ctx, containerPath, preferredBase)
	if err == nil && directRepo != nil {
		repos = append(repos, *directRepo)
		if ticketID == "" {
			ticketID = extractTicketID(directRepo.Branch)
		}
	} else {
		multiRepo = true
		entries, err := os.ReadDir(containerPath)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			childPath := filepath.Join(containerPath, entry.Name())
			rstate, err := e.AuditRepoState(ctx, childPath, preferredBase)
			if err == nil && rstate != nil {
				repos = append(repos, *rstate)
				if ticketID == "" {
					ticketID = extractTicketID(rstate.Branch)
				}
			}
		}
	}

	if len(repos) == 0 {
		return nil, nil
	}

	var planeStateGroup, planeStateName string
	if statusProvider != nil {
		planeStateGroup, planeStateName = statusProvider(ticketID)
	}
	ticketIsCancelled := planeStateGroup == "cancelled"

	var category workspace.ContainerCategory
	var reason string

	hasProtected := false
	allSafeToPrune := true
	allClean := true
	allMerged := true
	anyMerged := false

	for _, r := range repos {
		if r.IsProtected {
			hasProtected = true
		}
		if r.StatusCategory != "SAFE_TO_PRUNE" {
			allSafeToPrune = false
		}
		if !r.IsClean {
			allClean = false
		}
		if !r.IsMerged {
			allMerged = false
		} else {
			anyMerged = true
		}
	}

	if hasProtected {
		category = workspace.CategoryProtegido
		reason = "Contiene rama principal protegida (main/staging/dev)"
	} else if allSafeToPrune {
		category = workspace.CategorySeguroParaPodar
		reason = "Todos los repositorios están limpios y mergeados"
	} else if ticketIsCancelled {
		if allClean {
			category = workspace.CategoryAbandonadoLimpio
			reason = "Ticket cancelado en Plane, repositorios limpios"
		} else {
			category = workspace.CategoryAbandonadoCambios
			reason = "Ticket cancelado en Plane pero con cambios locales sin commitear"
		}
	} else if allMerged && !allClean {
		category = workspace.CategoryMergeadoConDirty
		reason = "Ramas mergeadas pero con archivos locales no commiteados"
	} else if anyMerged && !allMerged {
		category = workspace.CategoryParcialmenteMerge
		reason = "Algunos repositorios mergeados y otros en curso"
	} else if !allClean {
		category = workspace.CategoryBloqueadoPorCambios
		reason = "Ramas no integradas con archivos sin commitear"
	} else {
		category = workspace.CategoryEnCurso
		reason = "Ramas activas en desarrollo"
	}

	return &workspace.ContainerAudit{
		Container:       cname,
		TicketID:        ticketID,
		Path:            containerPath,
		Category:        category,
		Reason:          reason,
		PlaneStateGroup: planeStateGroup,
		PlaneStateName:  planeStateName,
		MultiRepo:       multiRepo,
		Repos:           repos,
	}, nil
}

// SyncWorktreeRemotes sincroniza referencias de origin en paralelo por repositorio base.
func (e *GitExecutor) SyncWorktreeRemotes(ctx context.Context, baseDir, target string, timeout time.Duration) {
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		return
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return
	}

	var candidateContainers []string
	targetUpper := strings.ToUpper(strings.TrimSpace(target))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		cp := filepath.Join(baseDir, entry.Name())
		if targetUpper != "" {
			if strings.ToUpper(entry.Name()) == targetUpper || strings.Contains(strings.ToUpper(entry.Name()), targetUpper) {
				candidateContainers = append(candidateContainers, cp)
			}
		} else {
			candidateContainers = append(candidateContainers, cp)
		}
	}

	var repoDirs []string
	for _, cp := range candidateContainers {
		root, _, err := e.runner.Run(ctx, cp, "rev-parse", "--show-toplevel")
		if err == nil && filepath.Clean(root) == filepath.Clean(cp) {
			repoDirs = append(repoDirs, cp)
		} else {
			children, _ := os.ReadDir(cp)
			for _, ch := range children {
				if ch.IsDir() {
					chPath := filepath.Join(cp, ch.Name())
					chRoot, _, err := e.runner.Run(ctx, chPath, "rev-parse", "--show-toplevel")
					if err == nil && filepath.Clean(chRoot) == filepath.Clean(chPath) {
						repoDirs = append(repoDirs, chPath)
					}
				}
			}
		}
	}

	uniqueRepos := make(map[string]string)
	for _, rpath := range repoDirs {
		gcd, _, err := e.runner.Run(ctx, rpath, "rev-parse", "--git-common-dir")
		if err == nil && strings.TrimSpace(gcd) != "" {
			gcdAbs, err := filepath.Abs(filepath.Join(rpath, strings.TrimSpace(gcd)))
			if err == nil {
				gcdReal, err := filepath.EvalSymlinks(gcdAbs)
				if err == nil && uniqueRepos[gcdReal] == "" {
					uniqueRepos[gcdReal] = rpath
				}
			}
		}
	}

	if len(uniqueRepos) == 0 {
		return
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)

	for _, path := range uniqueRepos {
		wg.Add(1)
		go func(repoPath string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			fetchCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			remotes, _, err := e.runner.Run(fetchCtx, repoPath, "remote")
			if err == nil && strings.Contains(remotes, "origin") {
				_, _, _ = e.runner.Run(fetchCtx, repoPath, "fetch", "origin", "--quiet")
			}
		}(path)
	}

	wg.Wait()
}

// AuditContainers inspecciona y audita todos los contenedores presentes en baseDir.
func (e *GitExecutor) AuditContainers(ctx context.Context, baseDir, target, preferredBase string, noFetch bool, statusProvider StatusProvider) ([]workspace.ContainerAudit, error) {
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(absBase); os.IsNotExist(err) {
		return []workspace.ContainerAudit{}, nil
	}

	if !noFetch {
		e.SyncWorktreeRemotes(ctx, absBase, target, 10*time.Second)
	}

	entries, err := os.ReadDir(absBase)
	if err != nil {
		return nil, fmt.Errorf("error leyendo directorio de worktrees '%s': %w", absBase, err)
	}

	var audited []workspace.ContainerAudit
	targetUpper := strings.ToUpper(strings.TrimSpace(target))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		cPath := filepath.Join(absBase, entry.Name())
		cAudit, err := e.AuditContainer(ctx, cPath, preferredBase, statusProvider)
		if err != nil || cAudit == nil {
			continue
		}

		if targetUpper != "" {
			match := strings.ToUpper(cAudit.Container) == targetUpper ||
				(cAudit.TicketID != "" && strings.ToUpper(cAudit.TicketID) == targetUpper) ||
				strings.Contains(strings.ToUpper(cAudit.Container), targetUpper)
			if !match {
				continue
			}
		}

		audited = append(audited, *cAudit)
	}

	sort.Slice(audited, func(i, j int) bool {
		return audited[i].Container < audited[j].Container
	})

	return audited, nil
}

// PruneContainers ejecuta o simula la poda inteligente de contenedores de worktree.
func (e *GitExecutor) PruneContainers(ctx context.Context, baseDir, target, preferredBase string, execute, includeStale, noFetch bool, backupDir string, statusProvider StatusProvider) (*workspace.PruneResult, error) {
	containers, err := e.AuditContainers(ctx, baseDir, target, preferredBase, noFetch, statusProvider)
	if err != nil {
		return nil, err
	}

	res := &workspace.PruneResult{
		Mode:         "dry-run",
		PrunedSafe:   make([]string, 0),
		PrunedStale:  make([]string, 0),
		SkippedStale: make([]string, 0),
		Blocked:      make([]workspace.BlockedContainer, 0),
		Backups:      make([]string, 0),
	}
	if execute {
		res.Mode = "execute"
	}

	if backupDir == "" {
		home, _ := os.UserHomeDir()
		backupDir = filepath.Join(home, ".local", "share", "allostasis", "backups")
	}

	var toPruneSafe []workspace.ContainerAudit
	var toPruneStale []workspace.ContainerAudit

	for _, c := range containers {
		switch c.Category {
		case workspace.CategorySeguroParaPodar:
			toPruneSafe = append(toPruneSafe, c)
		case workspace.CategoryAbandonadoCambios, workspace.CategoryAbandonadoLimpio, workspace.CategoryMergeadoConDirty:
			if includeStale {
				toPruneStale = append(toPruneStale, c)
			} else {
				res.SkippedStale = append(res.SkippedStale, c.Container)
			}
		default:
			res.Blocked = append(res.Blocked, workspace.BlockedContainer{
				Container: c.Container,
				Category:  c.Category,
				Reason:    c.Reason,
			})
		}
	}

	for _, c := range toPruneSafe {
		res.PrunedSafe = append(res.PrunedSafe, c.Container)
	}
	for _, c := range toPruneStale {
		res.PrunedStale = append(res.PrunedStale, c.Container)
	}

	if !execute {
		return res, nil
	}

	// Ejecutar poda real
	toPruneAll := append(toPruneSafe, toPruneStale...)
	for _, c := range toPruneAll {
		// Generar respaldo si tiene repos con cambios sucios
		for _, r := range c.Repos {
			if !r.IsClean {
				_ = os.MkdirAll(backupDir, 0755)
				timestamp := time.Now().Format("20060102-150405")
				tarGzName := fmt.Sprintf("%s-%s-%s.tar.gz", c.TicketID, r.Name, timestamp)
				tarGzPath := filepath.Join(backupDir, tarGzName)
				if err := createDirBackup(r.Path, tarGzPath); err == nil {
					res.Backups = append(res.Backups, tarGzPath)
				}
			}
			// Desvincular de git
			gcd, _, _ := e.runner.Run(ctx, r.Path, "rev-parse", "--git-common-dir")
			if strings.TrimSpace(gcd) != "" {
				repoRoot := filepath.Dir(filepath.Clean(filepath.Join(r.Path, strings.TrimSpace(gcd))))
				_, _, _ = e.runner.Run(ctx, repoRoot, "worktree", "remove", "--force", r.Path)
			}
		}

		// Eliminar directorio contenedor
		_ = os.RemoveAll(c.Path)
	}

	return res, nil
}

// createDirBackup comprime un directorio excluyendo .git a tar.gz
func createDirBackup(srcDir, tarGzFile string) error {
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

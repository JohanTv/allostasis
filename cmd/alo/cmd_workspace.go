package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/JohanTv/allostasis/internal/app"
	"github.com/JohanTv/allostasis/internal/domain/workspace"
	"github.com/JohanTv/allostasis/internal/presentation/errors"
	"github.com/spf13/cobra"
)

func newWorkspaceCmd() *cobra.Command {
	wsCmd := &cobra.Command{
		Use:     "workspace",
		Aliases: []string{"ws"},
		Short:   "Orquesta entornos multi-repo y Git worktrees transaccionales",
	}

	// 1. ws list
	wsListCmd := &cobra.Command{
		Use:   "list",
		Short: "Lista los worktrees activos por ticket",
		Run: func(cmd *cobra.Command, args []string) {
			wsSvc, store, err := initWorkspaceService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			containers, err := wsSvc.List(ctx, ".worktrees")
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "LIST_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				if containers == nil {
					containers = make([]workspace.Container, 0)
				}
				errors.EmitSuccess(containers, true)
				return
			}

			if len(containers) == 0 {
				fmt.Println("No hay contenedores de worktree activos en .worktrees/")
				return
			}

			fmt.Printf("🌳 %d Contenedores de Worktree Activos:\n", len(containers))
			for _, c := range containers {
				fmt.Printf("\n📁 Contenedor: %s (Ruta: %s)\n", c.Slug, c.Path)
				for _, b := range c.Bindings {
					status := "Limpio"
					if !b.IsClean {
						status = "Modificado"
					}
					fmt.Printf("   • %-14s [%s] Commit: %s (%s)\n", b.RepoName, b.Branch, b.HeadCommit, status)
				}
			}
		},
	}

	// 2. ws create / add
	var baseRefFlag string
	var typeFlag string
	wsCreateCmd := &cobra.Command{
		Use:     "create <TICKET-ID> [SLUG] [REPOS...]",
		Aliases: []string{"add"},
		Short:   "Crea un contenedor transaccional de worktrees para un ticket (alias: add)",
		Args:    cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			wsSvc, store, err := initWorkspaceService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			ticketID := strings.ToUpper(args[0])
			var slug string
			var repoTokens []string

			if len(args) >= 2 {
				// Detectar si el segundo argumento es un slug o un repositorio
				if strings.Contains(args[1], "/") || args[1] == "." {
					slug = strings.ToLower(ticketID)
					repoTokens = args[1:]
				} else {
					slug = fmt.Sprintf("%s-%s", strings.ToLower(ticketID), strings.ToLower(args[1]))
					repoTokens = args[2:]
				}
			} else {
				slug = strings.ToLower(ticketID)
			}

			var expandedRepos []string
			for _, token := range repoTokens {
				for _, part := range strings.Split(token, ",") {
					if trimmed := strings.TrimSpace(part); trimmed != "" {
						expandedRepos = append(expandedRepos, trimmed)
					}
				}
			}
			repoTokens = expandedRepos

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			container, err := wsSvc.Create(ctx, app.CreateContainerInput{
				TicketID:   ticketID,
				Slug:       slug,
				RepoTokens: repoTokens,
				BranchType: typeFlag,
				BaseRef:    baseRefFlag,
			})
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:        "WORKTREE_CREATION_FAILED",
					Category:    errors.CategoryStateConflict,
					Hint:        "Asegúrate de que la rama base exista y no haya colisiones de nombre",
					Recoverable: true,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(container, true)
				return
			}

			fmt.Printf("✓ Contenedor creado exitosamente en %s\n", container.Path)
			for _, b := range container.Bindings {
				fmt.Printf("  • %s -> rama: %s\n", b.RepoName, b.Branch)
			}
		},
	}
	wsCreateCmd.Flags().StringVar(&baseRefFlag, "base", "main", "Rama base upstream (ej. main, dev)")
	wsCreateCmd.Flags().StringVar(&typeFlag, "type", "feat", "Tipo de rama (feat, fix, spike, refactor, test, infra)")

	// 3. ws audit
	var wsAuditDirFlag string
	var wsAuditBaseFlag string
	var wsAuditNoFetchFlag bool
	wsAuditCmd := &cobra.Command{
		Use:   "audit [TARGET]",
		Short: "Audita contenedores de worktrees verificando merge status y salud local",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			wsSvc, store, err := initWorkspaceService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			target := ""
			if len(args) > 0 {
				target = args[0]
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			containers, err := wsSvc.Audit(ctx, wsAuditDirFlag, target, wsAuditBaseFlag, wsAuditNoFetchFlag)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:        "AUDIT_FAILED",
					Category:    errors.CategoryInternal,
					Recoverable: true,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if containers == nil {
				containers = make([]workspace.ContainerAudit, 0)
			}

			if jsonFlag {
				errors.EmitSuccess(containers, true)
				return
			}

			if len(containers) == 0 {
				if target != "" {
					fmt.Printf("No se encontró el contenedor o ticket '%s' en '%s'.\n", target, wsAuditDirFlag)
				} else {
					fmt.Printf("Sin worktrees activos para auditar en '%s'.\n", wsAuditDirFlag)
				}
				return
			}

			fmt.Printf("Auditoría de worktrees en '%s':\n\n", wsAuditDirFlag)
			for _, c := range containers {
				fmt.Printf("  • %s [%s: %s]\n", c.Container, c.Category, c.Reason)
				for _, r := range c.Repos {
					fmt.Printf("     └── %s [%s] (merged: %v, clean: %v)\n", r.Name, r.Branch, r.IsMerged, r.IsClean)
				}
			}
		},
	}
	wsAuditCmd.Flags().StringVar(&wsAuditDirFlag, "dir", ".worktrees", "Directorio base de contenedores")
	wsAuditCmd.Flags().StringVar(&wsAuditBaseFlag, "base", "", "Rama base preferida")
	wsAuditCmd.Flags().BoolVar(&wsAuditNoFetchFlag, "no-fetch", false, "Omitir git fetch previo")

	// 4. ws prune
	var wsPruneDirFlag string
	var wsPruneBaseFlag string
	var wsPruneExecuteFlag bool
	var wsPruneIncludeStaleFlag bool
	var wsPruneNoFetchFlag bool
	var wsPruneBackupDirFlag string
	wsPruneCmd := &cobra.Command{
		Use:   "prune [TARGET]",
		Short: "Desmantela o simula la eliminación de worktrees",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			wsSvc, store, err := initWorkspaceService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "INIT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()

			target := ""
			if len(args) > 0 {
				target = args[0]
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			res, err := wsSvc.Prune(ctx, wsPruneDirFlag, target, wsPruneBaseFlag, wsPruneExecuteFlag, wsPruneIncludeStaleFlag, wsPruneNoFetchFlag, wsPruneBackupDirFlag)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:        "PRUNE_FAILED",
					Category:    errors.CategoryStateConflict,
					Recoverable: true,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(res, true)
				return
			}

			if !wsPruneExecuteFlag {
				fmt.Printf("Simulación de poda (dry-run): %d seguro(s), %d stale(s), %d bloqueado(s)\n", len(res.PrunedSafe), len(res.PrunedStale), len(res.Blocked))
				return
			}

			fmt.Printf("✔ Poda ejecutada: %d contenedor(es) eliminados.\n", len(res.PrunedSafe)+len(res.PrunedStale))
		},
	}
	wsPruneCmd.Flags().StringVar(&wsPruneDirFlag, "dir", ".worktrees", "Directorio base de contenedores")
	wsPruneCmd.Flags().StringVar(&wsPruneBaseFlag, "base", "", "Rama base preferida")
	wsPruneCmd.Flags().BoolVar(&wsPruneExecuteFlag, "execute", false, "Ejecutar la poda destructiva en disco")
	wsPruneCmd.Flags().BoolVar(&wsPruneIncludeStaleFlag, "include-stale", false, "Podar también contenedores stale/cancelados")
	wsPruneCmd.Flags().BoolVar(&wsPruneNoFetchFlag, "no-fetch", false, "Omitir git fetch previo")
	wsPruneCmd.Flags().StringVar(&wsPruneBackupDirFlag, "backup-dir", "", "Directorio para respaldos tar.gz")

	wsCmd.AddCommand(wsListCmd, wsCreateCmd, wsAuditCmd, wsPruneCmd)
	return wsCmd
}

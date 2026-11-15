package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/allostasis/alo/internal/adapters/gitcli"
	"github.com/allostasis/alo/internal/domain/workspace"
	"github.com/allostasis/alo/internal/presentation/errors"
	"github.com/spf13/cobra"
)

var (
	Version   = "0.1.0"
	BuildDate = "2026-11-15"
	jsonFlag  bool
)

func main() {
	runner := gitcli.NewRunner(30 * time.Second)
	gitExec := gitcli.NewGitExecutor(runner)

	rootCmd := &cobra.Command{
		Use:   "alo-ws",
		Short: "Allostasis Worktree Orchestrator - Standalone Multi-Repo Workspace Manager",
		Long: `alo-ws es el orquestador independiente de entornos multi-repo de Allostasis:
Aísla tus ramas por ticket en contenedores .worktrees/, ejecuta pre-flights transaccionales,
audita merges trifásicos en memoria y protege tu código con respaldos automáticos .tar.gz.`,
	}

	rootCmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "Salida estructurada estricta en JSON para agentes de IA")

	// 1. Version
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Muestra la versión de alo-ws",
		Run: func(cmd *cobra.Command, args []string) {
			payload := map[string]string{
				"app":        "alo-ws",
				"version":    Version,
				"build_date": BuildDate,
			}
			if jsonFlag {
				errors.EmitSuccess(payload, true)
				return
			}
			fmt.Printf("Allostasis Worktree (alo-ws) v%s (compilado: %s)\n", Version, BuildDate)
		},
	}

	// 2. List
	var listDirFlag string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Lista los contenedores y worktrees activos",
		Run: func(cmd *cobra.Command, args []string) {
			ctx := context.Background()
			containers, err := gitExec.ListContainers(ctx, listDirFlag)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:        "WORKSPACE_LIST_FAILED",
					Category:    errors.CategoryInternal,
					Hint:        fmt.Sprintf("Verifica permisos de lectura en el directorio '%s/'", listDirFlag),
					Recoverable: true,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if containers == nil {
				containers = make([]workspace.Container, 0)
			}

			if jsonFlag {
				errors.EmitSuccess(containers, true)
				return
			}

			if len(containers) == 0 {
				fmt.Printf("No hay contenedores de worktree activos en %s/\n", listDirFlag)
				return
			}

			fmt.Printf("🌳 %d Contenedores de Worktree Activos en %s/:\n", len(containers), listDirFlag)
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
	listCmd.Flags().StringVar(&listDirFlag, "dir", ".worktrees", "Directorio base de contenedores de worktrees")

	// 3. Create / Add
	var baseRefFlag string
	var typeFlag string
	createHandler := func(cmd *cobra.Command, args []string) {
		ticketID := strings.ToUpper(args[0])
		var slug string
		var repoTokens []string

		// Soporte sintaxis dual:
		// 1) alo-ws create TICKET [REPOS...]
		// 2) plane-worktree add TICKET SLUG REPOS...
		if len(args) >= 2 {
			// Comprobar si args[1] es un repo resoluble o un slug descriptivo
			ctx := context.Background()
			_, err := gitExec.ResolveRepo(ctx, args[1])
			if err == nil {
				// Es un repo token directo
				slug = strings.ToLower(ticketID)
				repoTokens = args[1:]
			} else {
				// Es un slug descriptivo
				slug = fmt.Sprintf("%s-%s", strings.ToLower(ticketID), strings.ToLower(args[1]))
				repoTokens = args[2:]
			}
		} else {
			slug = strings.ToLower(ticketID)
		}

		if len(repoTokens) == 0 {
			repoTokens = []string{"."}
		}

		ctx := context.Background()
		var bindings []workspace.WorktreeBinding

		branchPrefix := "feat"
		if typeFlag != "" {
			branchPrefix = typeFlag
		}

		for _, tok := range repoTokens {
			resolved, err := gitExec.ResolveRepo(ctx, tok)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:             "REPO_NOT_FOUND",
					Category:         errors.CategoryUserInput,
					Target:           tok,
					Hint:             fmt.Sprintf("Verifica que '%s' sea un repositorio Git válido o una ruta accesible", tok),
					SuggestedCommand: fmt.Sprintf("alo-ws create %s .", ticketID),
					Recoverable:      true,
				}, errors.ExitNotFound, jsonFlag)
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
			BaseRef:  baseRefFlag,
			Bindings: bindings,
		}

		if err := gitExec.CreateWorktree(ctx, container); err != nil {
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

		fmt.Printf("✓ Contenedor creado exitosamente en %s\n", containerPath)
		for _, b := range container.Bindings {
			fmt.Printf("  • %s -> rama: %s\n", b.RepoName, b.Branch)
		}
	}

	createCmd := &cobra.Command{
		Use:     "create <TICKET-ID> [SLUG] [REPOS...]",
		Aliases: []string{"add"},
		Short:   "Crea un contenedor transaccional de worktrees para un ticket (alias: add)",
		Args:    cobra.MinimumNArgs(1),
		Run:     createHandler,
	}
	createCmd.Flags().StringVar(&baseRefFlag, "base", "main", "Rama base upstream (ej. main, dev)")
	createCmd.Flags().StringVar(&typeFlag, "type", "feat", "Tipo de rama (feat, fix, spike, refactor, test, infra)")

	// 4. Audit
	var auditDirFlag string
	var auditBaseFlag string
	var auditNoFetchFlag bool
	auditCmd := &cobra.Command{
		Use:   "audit [TARGET]",
		Short: "Audita contenedores de worktrees verificando merge status y salud local",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			target := ""
			if len(args) > 0 {
				target = args[0]
			}

			ctx := context.Background()
			containers, err := gitExec.AuditContainers(ctx, auditDirFlag, target, auditBaseFlag, "", auditNoFetchFlag)
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
					fmt.Printf("No se encontró el contenedor o ticket '%s' en '%s'.\n", target, auditDirFlag)
				} else {
					fmt.Printf("Sin worktrees activos para auditar en '%s'.\n", auditDirFlag)
				}
				return
			}

			fmt.Printf("Auditoría de worktrees en '%s':\n\n", auditDirFlag)

			groups := map[workspace.ContainerCategory]struct {
				header string
				items  []workspace.ContainerAudit
			}{
				workspace.CategorySeguroParaPodar:     {header: "✔ [SEGURO PARA PODAR] (100% integrados y limpios):", items: nil},
				workspace.CategoryAbandonadoCambios:   {header: "⚠ [ABANDONADOS CON CAMBIOS] (Cancelados/stale con cambios locales; exigen --include-stale):", items: nil},
				workspace.CategoryAbandonadoLimpio:    {header: "⚠ [ABANDONADOS LIMPIOS] (Cancelados/stale limpios; exigen --include-stale para podar):", items: nil},
				workspace.CategoryMergeadoConDirty:    {header: "✖ [MERGEADOS CON CAMBIOS] (Integrados en Git pero con cambios locales sin commitear):", items: nil},
				workspace.CategoryEnCurso:             {header: "○ [EN CURSO / ACTIVOS] (Ramas vivas en desarrollo):", items: nil},
				workspace.CategoryParcialmenteMerge:   {header: "◐ [PARCIALMENTE MERGEADO] (Algunos repositorios mergeados y otros en curso):", items: nil},
				workspace.CategoryBloqueadoPorCambios: {header: "✖ [BLOQUEADOS POR CAMBIOS] (Ramas no integradas con cambios sin commitear):", items: nil},
				workspace.CategoryProtegido:           {header: "🛡 [RAMA PRINCIPAL PROTEGIDA] (Inmune a poda):", items: nil},
			}

			for _, c := range containers {
				g := groups[c.Category]
				g.items = append(g.items, c)
				groups[c.Category] = g
			}

			order := []workspace.ContainerCategory{
				workspace.CategorySeguroParaPodar,
				workspace.CategoryAbandonadoCambios,
				workspace.CategoryAbandonadoLimpio,
				workspace.CategoryMergeadoConDirty,
				workspace.CategoryEnCurso,
				workspace.CategoryParcialmenteMerge,
				workspace.CategoryBloqueadoPorCambios,
				workspace.CategoryProtegido,
			}

			for _, cat := range order {
				g := groups[cat]
				if len(g.items) == 0 {
					continue
				}
				fmt.Printf("  %s\n", g.header)
				for _, c := range g.items {
					ticketSuffix := ""
					if c.TicketID != "" {
						ticketSuffix = fmt.Sprintf(" [%s]", c.TicketID)
					}
					fmt.Printf("  • %s/%s\n", c.Container, ticketSuffix)
					for idx, r := range c.Repos {
						conn := "├──"
						if idx == len(c.Repos)-1 {
							conn = "└──"
						}
						cleanStr := "clean"
						if !r.IsClean {
							cleanStr = fmt.Sprintf("dirty: %d archivo(s)", r.DirtyCount)
						}
						mergeStr := "no mergeado"
						if r.IsMerged {
							mergeStr = fmt.Sprintf("mergeado a %s (%s)", r.BaseRef, r.MergeMethod)
						}
						fmt.Printf("     %s %s [%s] (%s; %s)\n", conn, r.Name, r.Branch, mergeStr, cleanStr)
					}
				}
				fmt.Println()
			}

			safeCnt := len(groups[workspace.CategorySeguroParaPodar].items)
			staleCnt := len(groups[workspace.CategoryAbandonadoCambios].items) + len(groups[workspace.CategoryAbandonadoLimpio].items)
			fmt.Printf("Resumen: %d contenedor(es) podable(s) de inmediato.\n", safeCnt)
			if staleCnt > 0 {
				fmt.Printf("         %d contenedor(es) cancelado(s)/abandonado(s) podable(s) con --include-stale.\n", staleCnt)
			}
			fmt.Println("Para simular la poda ejecuta: alo-ws prune")
		},
	}
	auditCmd.Flags().StringVar(&auditDirFlag, "dir", ".worktrees", "Directorio base de contenedores")
	auditCmd.Flags().StringVar(&auditBaseFlag, "base", "", "Rama base preferida para comparación (ej. dev, main)")
	auditCmd.Flags().BoolVar(&auditNoFetchFlag, "no-fetch", false, "Omitir git fetch de origin")

	// 5. Prune
	var pruneDirFlag string
	var pruneExecuteFlag bool
	var pruneIncludeStaleFlag bool
	var pruneNoFetchFlag bool
	var pruneBackupDirFlag string
	pruneCmd := &cobra.Command{
		Use:   "prune [TARGET]",
		Short: "Podar de forma segura o simular la eliminación de worktrees completados",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			target := ""
			if len(args) > 0 {
				target = args[0]
			}

			ctx := context.Background()
			res, err := gitExec.PruneContainers(ctx, pruneDirFlag, target, pruneExecuteFlag, pruneIncludeStaleFlag, pruneNoFetchFlag, pruneBackupDirFlag)
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

			if !pruneExecuteFlag {
				fmt.Printf("Simulación de poda de worktrees (DRY-RUN) en '%s':\n\n", pruneDirFlag)
				if len(res.PrunedSafe) > 0 {
					fmt.Println("  ✔ Candidatos seguros para podar (100% integrados y limpios):")
					for _, c := range res.PrunedSafe {
						fmt.Printf("    • %s\n", c)
					}
				}
				if len(res.PrunedStale) > 0 {
					fmt.Println("  ⚠ Cancelados/stale seleccionados con --include-stale:")
					for _, c := range res.PrunedStale {
						fmt.Printf("    • %s\n", c)
					}
				}
				if len(res.SkippedStale) > 0 {
					fmt.Println("  ⚠ Omitidos (cancelados/stale; requieren flag --include-stale):")
					for _, c := range res.SkippedStale {
						fmt.Printf("    • %s\n", c)
					}
				}
				if len(res.Blocked) > 0 {
					fmt.Println("  ✖ Bloqueados ante poda (ramas activas con cambios pendientes):")
					for _, b := range res.Blocked {
						fmt.Printf("    • %s (%s: %s)\n", b.Container, b.Category, b.Reason)
					}
				}
				totalActionable := len(res.PrunedSafe) + len(res.PrunedStale)
				fmt.Printf("\nResumen dry-run: %d contenedor(es) listos para podar.\n", totalActionable)
				fmt.Println("Para ejecutar la poda real agrega la bandera: --execute")
				return
			}

			fmt.Printf("✔ Poda ejecutada exitosamente en '%s':\n", pruneDirFlag)
			for _, c := range res.PrunedSafe {
				fmt.Printf("  • Eliminado seguro: %s\n", c)
			}
			for _, c := range res.PrunedStale {
				fmt.Printf("  • Eliminado stale: %s\n", c)
			}
			if len(res.Backups) > 0 {
				fmt.Println("\n📦 Respaldos de seguridad generados:")
				for _, b := range res.Backups {
					fmt.Printf("  • %s\n", b)
				}
			}
		},
	}
	pruneCmd.Flags().StringVar(&pruneDirFlag, "dir", ".worktrees", "Directorio base de contenedores")
	pruneCmd.Flags().BoolVar(&pruneExecuteFlag, "execute", false, "Ejecutar la poda destructiva en disco (por defecto es dry-run)")
	pruneCmd.Flags().BoolVar(&pruneIncludeStaleFlag, "include-stale", false, "Podar también contenedores cancelados o stale con respaldo automático")
	pruneCmd.Flags().BoolVar(&pruneNoFetchFlag, "no-fetch", false, "Omitir git fetch previo")
	pruneCmd.Flags().StringVar(&pruneBackupDirFlag, "backup-dir", "", "Directorio para respaldos .tar.gz")

	rootCmd.AddCommand(versionCmd, listCmd, createCmd, auditCmd, pruneCmd)

	errors.SafeExecute(jsonFlag, func() {
		if err := rootCmd.Execute(); err != nil {
			os.Exit(1)
		}
	})
}

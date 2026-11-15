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
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Lista los contenedores y worktrees activos",
		Run: func(cmd *cobra.Command, args []string) {
			ctx := context.Background()
			containers, err := gitExec.ListContainers(ctx, ".worktrees")
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:        "WORKSPACE_LIST_FAILED",
					Category:    errors.CategoryInternal,
					Hint:        "Verifica permisos de lectura en el directorio .worktrees/",
					Recoverable: true,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
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

	// 3. Create
	var baseRefFlag string
	createCmd := &cobra.Command{
		Use:   "create <TICKET-ID> [REPOS...]",
		Short: "Crea un contenedor transaccional de worktrees para un ticket",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ticketID := strings.ToUpper(args[0])
			repoTokens := args[1:]
			if len(repoTokens) == 0 {
				repoTokens = []string{"."}
			}

			ctx := context.Background()
			var bindings []workspace.WorktreeBinding

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
				branchName := fmt.Sprintf("feat/%s", strings.ToLower(ticketID))

				bindings = append(bindings, workspace.WorktreeBinding{
					RepoName: repoName,
					RepoPath: resolved,
					Branch:   branchName,
				})
			}

			containerPath := filepath.Join(".worktrees", strings.ToLower(ticketID))
			container := &workspace.Container{
				TicketID: ticketID,
				Slug:     strings.ToLower(ticketID),
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
		},
	}
	createCmd.Flags().StringVar(&baseRefFlag, "base", "main", "Rama base upstream (ej. main, dev)")

	// 4. Prune
	var backupFlag bool
	pruneCmd := &cobra.Command{
		Use:   "prune <TICKET-ID>",
		Short: "Desmantela de forma segura un contenedor de worktree",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ticketID := strings.ToLower(args[0])
			containerPath := filepath.Join(".worktrees", ticketID)

			if fi, err := os.Stat(containerPath); err != nil || !fi.IsDir() {
				errors.EmitErrorWithDetail(fmt.Errorf("el contenedor '%s' no existe", containerPath), errors.ErrorDetail{
					Code:             "CONTAINER_NOT_FOUND",
					Category:         errors.CategoryUserInput,
					Target:           ticketID,
					Hint:             "Ejecuta 'alo-ws list' para ver los contenedores disponibles",
					SuggestedCommand: "alo-ws list",
					Recoverable:      true,
				}, errors.ExitNotFound, jsonFlag)
			}

			ctx := context.Background()
			containers, err := gitExec.ListContainers(ctx, ".worktrees")
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:        "WORKSPACE_AUDIT_FAILED",
					Category:    errors.CategoryInternal,
					Recoverable: true,
				}, errors.ExitGeneralError, jsonFlag)
			}

			var targetContainer *workspace.Container
			for i := range containers {
				if strings.EqualFold(containers[i].Slug, ticketID) {
					targetContainer = &containers[i]
					targetContainer.TicketID = strings.ToUpper(ticketID)
					break
				}
			}

			if targetContainer == nil {
				errors.EmitErrorWithDetail(fmt.Errorf("contenedor no reconocido en .worktrees/"), errors.ErrorDetail{
					Code:             "CONTAINER_UNRECOGNIZED",
					Category:         errors.CategoryUserInput,
					Hint:             "Verifica con 'alo-ws list' el identificador exacto",
					SuggestedCommand: "alo-ws list",
					Recoverable:      true,
				}, errors.ExitNotFound, jsonFlag)
			}

			backupDir := ""
			if backupFlag {
				home, _ := os.UserHomeDir()
				backupDir = filepath.Join(home, ".cache", "allostasis", "worktree-backups")
			}

			if err := gitExec.PruneWorktree(ctx, targetContainer, backupDir); err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:        "PRUNE_FAILED",
					Category:    errors.CategoryStateConflict,
					Hint:        "Verifica que ningún proceso tenga archivos abiertos en el contenedor",
					Recoverable: true,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				payload := map[string]string{
					"status":    "pruned",
					"ticket_id": targetContainer.TicketID,
					"path":      containerPath,
				}
				errors.EmitSuccess(payload, true)
				return
			}

			fmt.Printf("✓ Contenedor %s podado limpiamente.\n", containerPath)
		},
	}
	pruneCmd.Flags().BoolVar(&backupFlag, "backup", true, "Generar respaldo tar.gz si existen cambios no commiteados")

	rootCmd.AddCommand(versionCmd, listCmd, createCmd, pruneCmd)

	errors.SafeExecute(jsonFlag, func() {
		if err := rootCmd.Execute(); err != nil {
			os.Exit(1)
		}
	})
}

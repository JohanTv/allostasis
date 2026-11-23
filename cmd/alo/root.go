package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/JohanTv/allostasis/internal/adapters/gitcli"
	"github.com/JohanTv/allostasis/internal/adapters/sqlite"
	"github.com/JohanTv/allostasis/internal/adapters/tracker_plane"
	"github.com/JohanTv/allostasis/internal/app"
	"github.com/JohanTv/allostasis/internal/config"
	"github.com/JohanTv/allostasis/internal/domain/clock"
	"github.com/JohanTv/allostasis/internal/domain/issue"
	"github.com/JohanTv/allostasis/internal/presentation/errors"
	"github.com/spf13/cobra"
)

var (
	Version   = "0.1.0"
	BuildDate = "2026-11-15"
	jsonFlag  bool
	dbPath    string
)

func initCockpitService() (*app.CockpitService, *sqlite.Store, *gitcli.GitExecutor, error) {
	resolvedDB := config.ResolveDatabasePath(dbPath)
	config.ValidateTestSafety(resolvedDB)

	store, err := sqlite.NewStore(resolvedDB)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error al inicializar base de datos: %w", err)
	}

	runner := gitcli.NewRunner(30 * time.Second)
	gitExec := gitcli.NewGitExecutor(runner)
	clk := clock.NewRealClock()

	svc := app.NewCockpitService(store, store, gitExec, clk)
	return svc, store, gitExec, nil
}

func initTrackerService() (*app.TrackerService, *sqlite.Store, error) {
	resolvedDB := config.ResolveDatabasePath(dbPath)
	config.ValidateTestSafety(resolvedDB)

	store, err := sqlite.NewStore(resolvedDB)
	if err != nil {
		return nil, nil, fmt.Errorf("error al inicializar base de datos: %w", err)
	}

	planeCfg := tracker_plane.LoadConfigFromEnv()
	var client app.RemoteTrackerClient
	if planeCfg.APIKey != "" {
		client = tracker_plane.NewClient(planeCfg)
	}

	svc := app.NewTrackerService(store, client, clock.NewRealClock())
	return svc, store, nil
}

func initWorkspaceService() (*app.WorkspaceService, *sqlite.Store, error) {
	resolvedDB := config.ResolveDatabasePath(dbPath)
	config.ValidateTestSafety(resolvedDB)

	store, err := sqlite.NewStore(resolvedDB)
	if err != nil {
		return nil, nil, fmt.Errorf("error al inicializar base de datos: %w", err)
	}

	runner := gitcli.NewRunner(30 * time.Second)
	gitExec := gitcli.NewGitExecutor(runner)

	svc := app.NewWorkspaceService(gitExec, store)
	return svc, store, nil
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "alo",
		Version: Version,
		Short:   "Allostasis - The Sovereign Developer Cockpit for Flow and Cognitive Load Regulation",
		Long: `Allostasis (alo) es tu estación soberana de cabina:
Visibiliza el trabajo invisible, protege tu foco aplicando la Ley de Little,
y aísla tus ramas multi-repo mediante Git worktrees transaccionales.`,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "Salida estructurada estricta en JSON para agentes de IA")
	cmd.PersistentFlags().StringVar(&dbPath, "db", "", "Ruta personalizada a la base de datos SQLite")

	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newBrowseCmd())
	cmd.AddCommand(newAgendaCmd())
	cmd.AddCommand(newLoadCmd())
	cmd.AddCommand(newLogCmd())
	cmd.AddCommand(newSyncCmd())
	cmd.AddCommand(newWorkspaceCmd())
	cmd.AddCommand(newTicketCmd())
	cmd.AddCommand(newProjectsCmd())
	cmd.AddCommand(newStatesCmd())

	return cmd
}

func newStatesCmd() *cobra.Command {
	var statesProjectFlag string
	var statesRefreshFlag bool
	cmd := &cobra.Command{
		Use:     "states",
		Aliases: []string{"st"},
		Short:   "Listar los estados de workflow por proyecto",
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
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

			states, err := svc.States(ctx, statesProjectFlag, statesRefreshFlag)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "STATES_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if states == nil {
				states = make([]issue.StateRecord, 0)
			}

			if jsonFlag {
				payload := map[string]interface{}{
					"count":   len(states),
					"results": states,
					"schema":  "allostasis/1",
					"view":    "states",
				}
				errors.EmitSuccess(payload, true)
				return
			}

			if len(states) == 0 {
				fmt.Println("No se encontraron estados registrados. Ejecuta 'alo ticket sync' o usa '--refresh'.")
				return
			}

			fmt.Printf("%-10s %-12s %-22s %-18s %s\n", "PROYECTO", "GRUPO", "ESTADO", "SLUG", "ID")
			fmt.Println(strings.Repeat("─", 88))
			for _, st := range states {
				proj := st.ProjectKey
				if proj == "" {
					proj = st.ProjectID
				}
				defMark := ""
				if st.IsDefault {
					defMark = " *"
				}
				fmt.Printf("%-10s %-12s %-22s %-18s %s\n", proj, st.GroupName, st.Name+defMark, st.Slug, st.ID)
			}
		},
	}
	cmd.Flags().StringVarP(&statesProjectFlag, "project", "p", "", "Filtrar por proyecto (ej. OMNIFY, OPS)")
	cmd.Flags().BoolVar(&statesRefreshFlag, "refresh", false, "Refrescar estados desde Plane CE")
	return cmd
}

func newProjectsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "projects",
		Aliases: []string{"proj"},
		Short:   "Listar proyectos disponibles en el workspace",
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
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

			projects, err := svc.Projects(ctx)
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "PROJECTS_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(projects, true)
				return
			}

			if len(projects) == 0 {
				fmt.Println("No hay proyectos sincronizados. Ejecuta 'alo ticket sync' para descargar el catálogo.")
				return
			}

			printProjectsTable(projects)
		},
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Muestra la versión de Allostasis",
		Run: func(cmd *cobra.Command, args []string) {
			if jsonFlag {
				payload := map[string]string{
					"app":        "allostasis",
					"version":    Version,
					"build_date": BuildDate,
				}
				errors.EmitSuccess(payload, true)
				return
			}
			fmt.Printf("Allostasis (alo) v%s (compilado: %s)\n", Version, BuildDate)
		},
	}
}

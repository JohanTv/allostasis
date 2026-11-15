package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/allostasis/alo/internal/adapters/gitcli"
	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/adapters/tracker_plane"
	"github.com/allostasis/alo/internal/app"
	"github.com/allostasis/alo/internal/domain/clock"
	"github.com/allostasis/alo/internal/presentation/errors"
	"github.com/allostasis/alo/internal/presentation/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	Version   = "0.1.0"
	BuildDate = "2026-11-15"
	jsonFlag  bool
	dbPath    string
)

func initCockpitService() (*app.CockpitService, *sqlite.Store, *gitcli.GitExecutor, error) {
	if dbPath == "" {
		dbPath = app.DefaultDatabasePath()
	}

	store, err := sqlite.NewStore(dbPath)
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
	if dbPath == "" {
		dbPath = app.DefaultDatabasePath()
	}

	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("error al inicializar base de datos: %w", err)
	}

	planeCfg := tracker_plane.LoadConfigFromEnv()
	var client *tracker_plane.Client
	if planeCfg.APIKey != "" {
		client = tracker_plane.NewClient(planeCfg)
	}

	svc := app.NewTrackerService(store, client)
	return svc, store, nil
}

func initWorkspaceService() (*app.WorkspaceService, *sqlite.Store, error) {
	if dbPath == "" {
		dbPath = app.DefaultDatabasePath()
	}

	store, err := sqlite.NewStore(dbPath)
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
		Run: func(cmd *cobra.Command, args []string) {
			svc, store, _, err := initCockpitService()
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

			snap, err := svc.GetSnapshot(ctx, ".worktrees")
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{
					Code:     "SNAPSHOT_FAILED",
					Category: errors.CategoryInternal,
				}, errors.ExitGeneralError, jsonFlag)
			}

			if jsonFlag {
				errors.EmitSuccess(snap, true)
				return
			}

			if term.IsTerminal(int(os.Stdout.Fd())) {
				if err := tui.RunCockpit(svc, snap); err != nil {
					fmt.Fprintf(os.Stderr, "Error en interfaz TUI: %v\n", err)
					os.Exit(1)
				}
				return
			}

			fmt.Printf("🧬 [ALLOSTASIS] - %s | WIP: %d | Klee: %.1f hrs | Toil: %.1f%%\n",
				snap.Timestamp.Format("2006-01-02 15:04"),
				snap.AllostaticLoad.WIPCount,
				float64(snap.KleeMeasureMinutes)/60.0,
				snap.AllostaticLoad.ToilRatio()*100.0,
			)
		},
	}

	cmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "Salida estructurada estricta en JSON para agentes de IA")
	cmd.PersistentFlags().StringVar(&dbPath, "db", "", "Ruta personalizada a la base de datos SQLite")

	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newAgendaCmd())
	cmd.AddCommand(newLoadCmd())
	cmd.AddCommand(newLogCmd())
	cmd.AddCommand(newSyncCmd())
	cmd.AddCommand(newWorkspaceCmd())
	cmd.AddCommand(newTicketCmd())

	return cmd
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

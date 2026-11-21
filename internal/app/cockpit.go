package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/allostasis/alo/internal/domain/clock"
	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/domain/ledger"
	"github.com/allostasis/alo/internal/domain/workspace"
)

// CockpitSnapshot captura el estado holístico del desarrollador para CLI y TUI.
type CockpitSnapshot struct {
	Timestamp          time.Time             `json:"timestamp"`
	ActiveIssues       []issue.WorkItem      `json:"active_issues"`
	ActiveWorktrees    []workspace.Container `json:"active_worktrees"`
	TodayMinutes       int                   `json:"today_minutes"`
	KleeMeasureMinutes int                   `json:"klee_measure_minutes"`
	AllostaticLoad     ledger.AllostaticLoad `json:"allostatic_load"`
	CycleTimeDays      float64               `json:"cycle_time_days"`
	StatusSummary      string                `json:"status_summary"`
}

// CockpitService orquesta la lectura y mutaciones de alto nivel desacoplado de infraestructura.
type CockpitService struct {
	issueStore   CockpitStore
	worklogStore WorklogStore
	gitExec      WorkspaceRunner
	clock        clock.Clock
}

// NewCockpitService crea el orquestador principal inyectando puertos de dominio y aplicación.
func NewCockpitService(issueStore CockpitStore, worklogStore WorklogStore, gitExec WorkspaceRunner, clk clock.Clock) *CockpitService {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &CockpitService{
		issueStore:   issueStore,
		worklogStore: worklogStore,
		gitExec:      gitExec,
		clock:        clk,
	}
}

// GetSnapshot compila el estado alostático en tiempo real.
func (s *CockpitService) GetSnapshot(ctx context.Context, worktreeRoot string) (*CockpitSnapshot, error) {
	now := s.clock.Now()
	todayStr := now.Format("2006-01-02")

	// 1. Obtener tareas activas (in_progress, unstarted, backlog)
	allIssues, err := s.issueStore.ListIssues(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("error al listar tareas: %w", err)
	}

	activeIssues := make([]issue.WorkItem, 0)
	for _, it := range allIssues {
		if it.State == "started" || it.State == "in_progress" || it.State == "unstarted" {
			activeIssues = append(activeIssues, it)
		}
	}

	// 2. Obtener contenedores de worktree
	containers := make([]workspace.Container, 0)
	if worktreeRoot == "" {
		worktreeRoot = ".worktrees"
	}
	if s.gitExec != nil {
		c, err := s.gitExec.ListContainers(ctx, worktreeRoot)
		if err == nil && c != nil {
			containers = c
		}
	}

	// 3. Obtener worklogs de hoy
	worklogs, err := s.worklogStore.GetWorklogs(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("error al obtener worklogs: %w", err)
	}

	var todayIntervals []ledger.Interval
	var deepWorkIntervals []ledger.Interval
	var toilIntervals []ledger.Interval
	totalMins := 0

	for _, w := range worklogs {
		if w.Interval.Start.Format("2006-01-02") == todayStr {
			todayIntervals = append(todayIntervals, w.Interval)
			totalMins += w.Interval.Minutes()
			if w.Bucket == ledger.BucketToilSupport || string(w.Bucket) == "toil" || string(w.Bucket) == "overhead" {
				toilIntervals = append(toilIntervals, w.Interval)
			} else {
				deepWorkIntervals = append(deepWorkIntervals, w.Interval)
			}
		}
	}

	// 4. Medida de Klee
	kleeDuration := ledger.KleeMeasure(todayIntervals)
	deepDuration := ledger.KleeMeasure(deepWorkIntervals)
	toilDuration := ledger.KleeMeasure(toilIntervals)

	aloLoad := ledger.AllostaticLoad{
		TotalTime:    kleeDuration,
		DeepWorkTime: deepDuration,
		ToilTime:     toilDuration,
		WIPCount:     len(activeIssues),
	}

	cycleDays := ledger.LittlesLawCycleTime(aloLoad.WIPCount, 0.5)

	summary := "Estado nominal óptimo"
	if aloLoad.ToilRatio() > 0.4 {
		summary = "Sobrecarga de trabajo invisible (Toil crítico)"
	} else if aloLoad.WIPCount > 3 {
		summary = "Congestión por WIP elevado (Riesgo de cambio de contexto)"
	}

	return &CockpitSnapshot{
		Timestamp:          now,
		ActiveIssues:       activeIssues,
		ActiveWorktrees:    containers,
		TodayMinutes:       totalMins,
		KleeMeasureMinutes: int(kleeDuration.Minutes()),
		AllostaticLoad:     aloLoad,
		CycleTimeDays:      cycleDays,
		StatusSummary:      summary,
	}, nil
}

// LogTime registra una sesión en el ledger y encola el comentario/bitácora estructurado en outbox hacia Plane CE.
func (s *CockpitService) LogTime(ctx context.Context, ticketID string, durationMins int, bucket ledger.CapacityBucket, notes string) error {
	now := s.clock.Now()
	startTime := now.Add(-time.Duration(durationMins) * time.Minute)

	entry := &ledger.WorklogEntry{
		IssueID: ticketID,
		Interval: ledger.Interval{
			Start: startTime,
			End:   now,
		},
		Bucket: bucket,
		Topic:  notes,
	}

	if err := s.worklogStore.SaveWorklog(ctx, entry); err != nil {
		return err
	}

	// Encolar bitácora transaccional en outbox hacia Plane CE con bloque PLANE_WORKLOG_METADATA
	if s.issueStore != nil {
		dateStr := startTime.Format("2006-01-02")
		startStr := startTime.Format("15:04")
		endStr := now.Format("15:04")
		bodyText := strings.TrimSpace(notes)
		if bodyText == "" {
			bodyText = fmt.Sprintf("Sesión de trabajo (%d min)", durationMins)
		}

		commentHTML := fmt.Sprintf("<p>%s</p>\n<!-- PLANE_WORKLOG_METADATA\n{\n  \"version\": \"1.0\",\n  \"date\": \"%s\",\n  \"start_time\": \"%s\",\n  \"end_time\": \"%s\",\n  \"duration_minutes\": %d,\n  \"capacity_bucket\": \"%s\"\n}\n-->",
			bodyText, dateStr, startStr, endStr, durationMins, string(bucket))

		payload := map[string]string{
			"comment_html": commentHTML,
		}
		payloadBytes, _ := json.Marshal(payload)
		clientID := fmt.Sprintf("log-%s-%d", ticketID, now.UnixNano())
		_ = s.issueStore.EnqueueOutboxWithMutation(ctx, clientID, ticketID, string(payloadBytes), "", "comment")
	}

	return nil
}

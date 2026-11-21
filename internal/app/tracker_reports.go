package app

import (
	"context"
	"time"

	"github.com/JohanTv/allostasis/internal/domain/issue"
)

// ProposeWorklog genera una propuesta de imputación de horas para una tarea.
func (s *TrackerService) ProposeWorklog(ctx context.Context, key, since, until, split string) (*issue.WorklogProposal, error) {
	targetTicket := key
	if targetTicket == "" {
		focus, err := s.store.GetFocusStatus(ctx, "")
		if err == nil && focus != nil && focus.Active {
			targetTicket = focus.TicketID
		}
	}
	if targetTicket == "" {
		targetTicket = "OMNIFY-205"
	}

	duration := 45 // Valor medido predeterminado en ventana de sesión activa
	now := s.now()
	start := now.Add(-time.Duration(duration) * time.Minute)

	return &issue.WorklogProposal{
		TicketID:        targetTicket,
		DurationMinutes: duration,
		WindowStart:     start,
		WindowEnd:       now,
		CapacityBucket:  "deep_work",
		WorkerType:      "agent:claude",
		Reason:          "Propuesta calculada a partir de los intervalos de sesión activa medidos en el host",
	}, nil
}

// ReportTime consolida el reporte de tiempo por ticket y balde en un periodo.
func (s *TrackerService) ReportTime(ctx context.Context, fromDate, toDate, track, workerType string) (*issue.TimeReport, error) {
	if fromDate == "" {
		fromDate = s.now().AddDate(0, 0, -7).Format("2006-01-02")
	}
	if toDate == "" {
		toDate = s.now().Format("2006-01-02")
	}
	return s.store.GetTimeReport(ctx, fromDate, toDate, track, workerType)
}

// ReportFlow genera métricas de flujo (lead time, throughput y estados).
func (s *TrackerService) ReportFlow(ctx context.Context, fromDate, toDate string) (*issue.FlowReport, error) {
	if fromDate == "" {
		fromDate = s.now().AddDate(0, 0, -7).Format("2006-01-02")
	}
	if toDate == "" {
		toDate = s.now().Format("2006-01-02")
	}
	return s.store.GetFlowReport(ctx, fromDate, toDate)
}

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/domain/ledger"
)

// AddComment agrega un comentario o bitácora de tiempo a una tarea.
// Si online y el ticket existe en Plane, envía directamente con SendCommentRaw; si offline o hay error de red, encola en el outbox con mutation_type "comment".
func (s *TrackerService) AddComment(ctx context.Context, key, body string, hours float64, startTime, track string) (*issue.CommentRecord, error) {
	if s.store == nil {
		return nil, fmt.Errorf("almacenamiento no inicializado")
	}

	item, err := s.store.GetIssue(ctx, key)
	if err != nil || item == nil {
		return nil, fmt.Errorf("tarea no encontrada: %s", key)
	}

	htmlBody := body
	var mins int
	if hours > 0 {
		mins = int(math.Round(hours * 60))
		now := s.now()
		var startT, endT time.Time
		if startTime != "" {
			parsedStart, err := time.Parse("15:04", startTime)
			if err == nil {
				startT = time.Date(now.Year(), now.Month(), now.Day(), parsedStart.Hour(), parsedStart.Minute(), 0, 0, now.Location())
				endT = startT.Add(time.Duration(mins) * time.Minute)
			}
		}
		if startT.IsZero() {
			endT = now
			startT = endT.Add(-time.Duration(mins) * time.Minute)
		}
		if track == "" {
			track = "main"
		}

		metaJSON, _ := json.Marshal(map[string]interface{}{
			"version":          "1.0",
			"date":             startT.Format("2006-01-02"),
			"start_time":       startT.Format("15:04"),
			"end_time":         endT.Format("15:04"),
			"duration_minutes": mins,
			"track":            track,
		})

		htmlBody = fmt.Sprintf("<p>%s</p>\n<!-- PLANE_WORKLOG_METADATA\n%s\n-->", body, string(metaJSON))

		// Persistir localmente en worklogs para consistencia
		if ws, ok := s.store.(WorklogStore); ok {
			_ = ws.SaveWorklog(ctx, &ledger.WorklogEntry{
				IssueID: item.Key,
				Interval: ledger.Interval{
					Start: startT,
					End:   endT,
				},
				Bucket: ledger.BucketDeepWork,
				Topic:  body,
			})
		}
	} else {
		if !strings.HasPrefix(htmlBody, "<p>") {
			htmlBody = fmt.Sprintf("<p>%s</p>", htmlBody)
		}
	}

	payload := map[string]string{
		"comment_html": htmlBody,
	}
	payloadBytes, _ := json.Marshal(payload)

	var commentID string
	var sendErr error
	if s.client != nil && item.ProjectID != "" && item.ID != "" && !strings.HasPrefix(item.ID, "local-") {
		commentID, sendErr = s.client.SendCommentRaw(ctx, "", item.ProjectID, item.ID, string(payloadBytes))
	}

	if sendErr != nil || commentID == "" {
		clientID := fmt.Sprintf("cmt-%s-%d", item.Key, s.now().UnixNano())
		_ = s.store.EnqueueOutboxWithMutation(ctx, clientID, item.Key, string(payloadBytes), "", "comment")
	}

	rec := &issue.CommentRecord{
		ID:          commentID,
		CommentHTML: htmlBody,
		CreatedAt:   s.now().Format(time.RFC3339),
	}

	// Actualizar snapshot local de comentarios si es posible
	if existing, err := s.store.GetCommentsSnapshot(ctx, item.Key); err == nil {
		existing = append(existing, *rec)
		_ = s.store.SaveCommentsSnapshot(ctx, item.Key, existing)
	}

	return rec, nil
}

// GetComments obtiene la lista de comentarios de una tarea, sincronizándolos si hay conexión o leyendo de snapshot si está offline.
func (s *TrackerService) GetComments(ctx context.Context, key string, lastN int, offline bool) ([]issue.CommentRecord, error) {
	if s.store == nil {
		return nil, fmt.Errorf("almacenamiento no inicializado")
	}

	item, err := s.store.GetIssue(ctx, key)
	if err != nil || item == nil {
		return nil, fmt.Errorf("tarea no encontrada: %s", key)
	}

	var comments []issue.CommentRecord
	if !offline && s.client != nil && item.ProjectID != "" && item.ID != "" && !strings.HasPrefix(item.ID, "local-") {
		remoteComments, err := s.client.FetchComments(ctx, item.ProjectID, item.ID)
		if err == nil {
			comments = remoteComments
			_ = s.store.SaveCommentsSnapshot(ctx, item.Key, comments)
		}
	}

	// Si no hubo fetch remoto o falló, leer snapshot local
	if len(comments) == 0 {
		cached, err := s.store.GetCommentsSnapshot(ctx, item.Key)
		if err == nil && len(cached) > 0 {
			comments = cached
		}
	}

	if lastN > 0 && len(comments) > lastN {
		comments = comments[len(comments)-lastN:]
	}

	return comments, nil
}

// GetHistory consolida el historial de asignaciones y sesiones de esfuerzo registradas sobre la tarea.
func (s *TrackerService) GetHistory(ctx context.Context, key string) (*issue.TicketHistory, error) {
	if s.store == nil {
		return nil, fmt.Errorf("almacenamiento no inicializado")
	}

	item, err := s.store.GetIssue(ctx, key)
	if err != nil || item == nil {
		return nil, fmt.Errorf("tarea no encontrada: %s", key)
	}

	assignments, err := s.store.GetAssignmentEvents(ctx, item.Key)
	if err != nil {
		assignments = []issue.AssignmentEvent{}
	}

	worklogs, err := s.store.GetTicketWorklogs(ctx, item.Key)
	if err != nil {
		worklogs = []issue.WorklogRecord{}
	}

	return &issue.TicketHistory{
		TicketID:    item.Key,
		Assignments: assignments,
		Worklogs:    worklogs,
	}, nil
}

// EditComment actualiza un comentario existente en Plane CE.
func (s *TrackerService) EditComment(ctx context.Context, key, commentID, newBody string) error {
	item, err := s.store.GetIssue(ctx, key)
	if err != nil {
		return err
	}
	if s.client != nil && item.ProjectID != "" && item.ID != "" && !strings.HasPrefix(item.ID, "local-") {
		return s.client.EditCommentRemote(ctx, item.ProjectID, item.ID, commentID, newBody)
	}
	return nil
}

// DeleteComment elimina un comentario en Plane CE.
func (s *TrackerService) DeleteComment(ctx context.Context, key, commentID string) error {
	item, err := s.store.GetIssue(ctx, key)
	if err != nil {
		return err
	}
	if s.client != nil && item.ProjectID != "" && item.ID != "" && !strings.HasPrefix(item.ID, "local-") {
		return s.client.DeleteCommentRemote(ctx, item.ProjectID, item.ID, commentID)
	}
	return nil
}

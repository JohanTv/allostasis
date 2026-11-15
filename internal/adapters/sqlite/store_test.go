package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/domain/ledger"
)

func TestStore_InMemoryIntegration(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error creando store en memoria: %v", err)
	}
	defer store.Close()

	// 1. Probar inserción y consulta de tareas
	item := &issue.WorkItem{
		ID:          "uuid-123",
		Key:         "OMNIFY-100",
		Title:       "Validar adaptador Pure-Go SQLite",
		Description: "Sin dependencias CGO y con soporte FTS5",
		State:       issue.StateInProgress,
		Priority:    "high",
		Assignee:    "johan",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := store.SaveIssue(ctx, item); err != nil {
		t.Fatalf("error guardando tarea: %v", err)
	}

	retrieved, err := store.GetIssue(ctx, "OMNIFY-100")
	if err != nil {
		t.Fatalf("error obteniendo tarea: %v", err)
	}

	if retrieved.Title != item.Title || retrieved.State != item.State {
		t.Errorf("tarea recuperada no coincide: %+v", retrieved)
	}

	// 2. Probar inserción de Worklogs y cálculo continuo de Klee
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	entry1 := &ledger.WorklogEntry{
		IssueID:  "OMNIFY-100",
		Interval: ledger.Interval{Start: now, End: now.Add(45 * time.Minute)},
		Bucket:   ledger.BucketDeepWork,
		Topic:    "Pruebas unitarias de almacenamiento",
	}
	entry2 := &ledger.WorklogEntry{
		IssueID:  "OMNIFY-100",
		Interval: ledger.Interval{Start: now.Add(30 * time.Minute), End: now.Add(90 * time.Minute)}, // Solapado
		Bucket:   ledger.BucketDeepWork,
		Topic:    "Refactorización de conexiones",
	}

	if err := store.SaveWorklog(ctx, entry1); err != nil {
		t.Fatalf("error guardando worklog 1: %v", err)
	}
	if err := store.SaveWorklog(ctx, entry2); err != nil {
		t.Fatalf("error guardando worklog 2: %v", err)
	}

	intervals, err := store.GetIntervals(ctx)
	if err != nil {
		t.Fatalf("error obteniendo intervalos: %v", err)
	}

	if len(intervals) != 2 {
		t.Fatalf("esperados 2 intervalos, obtuvo %d", len(intervals))
	}

	// Fusión con Medida de Klee: [09:00 - 09:45] y [09:30 - 10:30] = [09:00 - 10:30] = 90 minutos
	totalDuration := ledger.KleeMeasure(intervals)
	if totalDuration != 90*time.Minute {
		t.Errorf("KleeMeasure esperado 90m, obtuvo %v", totalDuration)
	}
}

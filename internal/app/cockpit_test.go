package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/JohanTv/allostasis/internal/adapters/sqlite"
	"github.com/JohanTv/allostasis/internal/app"
	"github.com/JohanTv/allostasis/internal/domain/clock"
	"github.com/JohanTv/allostasis/internal/domain/issue"
	"github.com/JohanTv/allostasis/internal/domain/ledger"
)

func TestCockpitService_SnapshotAndLogTime(t *testing.T) {
	ctx := context.Background()

	// 1. Usar base en memoria
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error creando store en memoria: %v", err)
	}
	defer store.Close()

	// 2. Reloj congelado
	fixedTime := time.Date(2026, 11, 15, 14, 0, 0, 0, time.UTC)
	clk := clock.NewFrozenClock(fixedTime)

	svc := app.NewCockpitService(store, store, nil, clk)

	// 3. Crear tareas activas
	task1 := &issue.WorkItem{
		ID:    "task-1",
		Key:   "ALO-10",
		Title: "Refactorizar arquitectura en Go",
		State: "in_progress",
	}
	task2 := &issue.WorkItem{
		ID:    "task-2",
		Key:   "ALO-11",
		Title: "Implementar TUI interactiva con Lip Gloss",
		State: "started",
	}
	if err := store.SaveIssue(ctx, task1); err != nil {
		t.Fatalf("error guardando tarea 1: %v", err)
	}
	if err := store.SaveIssue(ctx, task2); err != nil {
		t.Fatalf("error guardando tarea 2: %v", err)
	}

	// 4. Registrar tiempo: 60 min de deep work y 30 min de toil
	if err := svc.LogTime(ctx, "ALO-10", 60, ledger.BucketDeepWork, "Codificando dominio hexagonal"); err != nil {
		t.Fatalf("error registrando deep work: %v", err)
	}
	if err := svc.LogTime(ctx, "ALO-11", 30, ledger.BucketToilSupport, "Resolviendo dependencias CGO"); err != nil {
		t.Fatalf("error registrando toil: %v", err)
	}

	// 5. Obtener snapshot
	snap, err := svc.GetSnapshot(ctx, "")
	if err != nil {
		t.Fatalf("error obteniendo snapshot: %v", err)
	}

	if len(snap.ActiveIssues) != 2 {
		t.Errorf("se esperaban 2 tareas activas, obtenidas %d", len(snap.ActiveIssues))
	}

	if snap.AllostaticLoad.WIPCount != 2 {
		t.Errorf("WIP esperado 2, obtenido %d", snap.AllostaticLoad.WIPCount)
	}

	if snap.TodayMinutes < 90 {
		t.Errorf("minutos de hoy esperados >= 90, obtenido %d", snap.TodayMinutes)
	}

	if snap.CycleTimeDays <= 0 {
		t.Errorf("tiempo de ciclo proyectado esperado > 0, obtenido %f", snap.CycleTimeDays)
	}
}

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

	// 3. Probar Búsqueda Tolerante (incluyendo descripción en snapshot)
	snapshotJSON := `{"id":"uuid-269","name":"[FEAT] Canal de atención Facebook Messenger e Instagram DM","description":"Integrar Facebook Messenger como redes sociales"}`
	item2 := &issue.WorkItem{
		ID:         "uuid-269",
		Key:        "OMNIFY-269",
		ProjectID:  "proj-omnify",
		Title:      "[FEAT] Canal de atención Facebook Messenger e Instagram DM",
		StateName:  "Backlog",
		StateGroup: "backlog",
		Priority:   "high",
		LabelsCSV:  "backend,feat",
	}
	if err := store.SaveIssue(ctx, item2); err != nil {
		t.Fatalf("error guardando issue 2: %v", err)
	}
	if err := store.SaveIssueSnapshot(ctx, item2.ID, item2.Key, snapshotJSON); err != nil {
		t.Fatalf("error guardando snapshot: %v", err)
	}

	// Buscar por "redes sociales" que NO está en el título pero SÍ en la descripción
	found, err := store.SearchIssues(ctx, "redes sociales", "OMNIFY", 10)
	if err != nil {
		t.Fatalf("error en SearchIssues: %v", err)
	}
	if len(found) != 1 || found[0].Key != "OMNIFY-269" {
		t.Fatalf("se esperaba encontrar OMNIFY-269 buscando por 'redes sociales', obtuvo: %+v", found)
	}

	// 4. Probar GetIssue con enriquecimiento de descripción
	loaded, err := store.GetIssue(ctx, "OMNIFY-269")
	if err != nil {
		t.Fatalf("error en GetIssue: %v", err)
	}
	if loaded.Description == "" {
		t.Errorf("GetIssue debió enriquecer la descripción desde el snapshot")
	}

	// 5. Probar Proyectos y Estados
	p := sqlite.ProjectRecord{Identifier: "OMNIFY", ProjectID: "proj-omnify", Name: "Omnify Platform", Workspace: "teamprogra"}
	if err := store.SaveProject(ctx, p); err != nil {
		t.Fatalf("error en SaveProject: %v", err)
	}
	pLoaded, err := store.GetProject(ctx, "OMNIFY")
	if err != nil || pLoaded.ProjectID != "proj-omnify" {
		t.Fatalf("error recuperando proyecto: %v", err)
	}

	st := sqlite.StateRecord{ID: "st-prog", ProjectID: "proj-omnify", Name: "Progreso", Slug: "progreso", GroupName: "started"}
	if err := store.SaveState(ctx, st); err != nil {
		t.Fatalf("error en SaveState: %v", err)
	}
	stLoaded, err := store.GetState(ctx, "proj-omnify", "Progreso")
	if err != nil || stLoaded.GroupName != "started" {
		t.Fatalf("error recuperando estado: %v", err)
	}

	// 6. Probar ListIssuesFiltered con OpenOnly y Assignee
	itemCompleted := &issue.WorkItem{
		ID:         "uuid-completed",
		Key:        "OMNIFY-101",
		ProjectID:  "proj-omnify",
		Title:      "Tarea completada",
		StateName:  "Completado",
		StateGroup: "completed",
		Priority:   "low",
		Assignees:  []string{"usr-other"},
	}
	itemActive := &issue.WorkItem{
		ID:         "uuid-active",
		Key:        "OMNIFY-102",
		ProjectID:  "proj-omnify",
		Title:      "Tarea en progreso",
		StateName:  "Progreso",
		StateGroup: "started",
		Priority:   "urgent",
		Assignees:  []string{"usr-johan", "usr-team"},
	}
	_ = store.SaveIssue(ctx, itemCompleted)
	_ = store.SaveIssue(ctx, itemActive)

	// Filtrar solo abiertos
	openItems, err := store.ListIssuesFiltered(ctx, issue.IssueFilter{ProjectKey: "OMNIFY", OpenOnly: true})
	if err != nil {
		t.Fatalf("error ListIssuesFiltered OpenOnly: %v", err)
	}
	for _, it := range openItems {
		if it.Key == "OMNIFY-101" {
			t.Errorf("ListIssuesFiltered OpenOnly no debió incluir tarea completada OMNIFY-101")
		}
	}

	// Filtrar por Assignee
	myItems, err := store.ListIssuesFiltered(ctx, issue.IssueFilter{ProjectKey: "OMNIFY", Assignee: "usr-johan"})
	if err != nil {
		t.Fatalf("error ListIssuesFiltered Assignee: %v", err)
	}
	if len(myItems) != 1 || myItems[0].Key != "OMNIFY-102" {
		t.Errorf("se esperaba 1 tarea para usr-johan (OMNIFY-102), obtenidos: %+v", myItems)
	}
}

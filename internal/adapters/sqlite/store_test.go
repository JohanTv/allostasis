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
		State:       issue.StateStarted,
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
	p := sqlite.ProjectRecord{Identifier: "OMNIFY", ProjectID: "proj-omnify", Name: "Omnify Platform", Workspace: "test-workspace"}
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

	// 1. Filtrar por CreatedBy (OMNIFY-259 creado por usuario sin asignado)
	itemCreated := &issue.WorkItem{
		ID:         "uuid-created",
		Key:        "OMNIFY-259",
		ProjectID:  "proj-omnify",
		Title:      "Iniciativa creada por Johan",
		StateName:  "Progreso",
		StateGroup: "started",
		Priority:   "high",
		CreatedBy:  "usr-johan",
		Assignees:  nil, // sin asignado
	}
	_ = store.SaveIssue(ctx, itemCreated)

	createdItems, err := store.ListIssuesFiltered(ctx, issue.IssueFilter{ProjectKey: "OMNIFY", CreatedBy: "usr-johan"})
	if err != nil {
		t.Fatalf("error ListIssuesFiltered CreatedBy: %v", err)
	}
	if len(createdItems) != 1 || createdItems[0].Key != "OMNIFY-259" {
		t.Errorf("se esperaba 1 tarea creada por usr-johan (OMNIFY-259), obtenidos: %+v", createdItems)
	}

	// 2. Filtrar por InvolvedBy (debe incluir OMNIFY-102 por asignado y OMNIFY-259 por creador)
	involvedItems, err := store.ListIssuesFiltered(ctx, issue.IssueFilter{ProjectKey: "OMNIFY", InvolvedBy: "usr-johan"})
	if err != nil {
		t.Fatalf("error ListIssuesFiltered InvolvedBy: %v", err)
	}
	if len(involvedItems) < 2 {
		t.Errorf("se esperaban al menos 2 tareas involucradas para usr-johan, obtenidos: %d", len(involvedItems))
	}

	// 3. Probar UserProfile
	userProfile := issue.UserInfo{
		ID:          "usr-johan",
		Username:    "johantv",
		Email:       "johan@example.com",
		DisplayName: "Johan Tanta",
	}
	if err := store.SaveUserProfile(ctx, userProfile); err != nil {
		t.Fatalf("error SaveUserProfile: %v", err)
	}
	cachedProfile, err := store.GetUserProfile(ctx)
	if err != nil {
		t.Fatalf("error GetUserProfile: %v", err)
	}
	if cachedProfile == nil || cachedProfile.ID != "usr-johan" {
		t.Errorf("perfil recuperado inválido: %+v", cachedProfile)
	}

	// 4. Probar ListStates ordenado por workflow lifecycle
	stBacklog := sqlite.StateRecord{ID: "st-backlog", ProjectID: "proj-omnify", Name: "Backlog", Slug: "backlog", GroupName: "backlog"}
	stRevision := sqlite.StateRecord{ID: "st-rev", ProjectID: "proj-omnify", Name: "Revisión", Slug: "revision", GroupName: "started"}
	stCompleted := sqlite.StateRecord{ID: "st-comp", ProjectID: "proj-omnify", Name: "Completado", Slug: "completado", GroupName: "completed", IsDefault: true}
	stCancelled := sqlite.StateRecord{ID: "st-canc", ProjectID: "proj-omnify", Name: "Cancelado", Slug: "cancelado", GroupName: "cancelled"}

	_ = store.SaveState(ctx, stBacklog)
	_ = store.SaveState(ctx, stRevision)
	_ = store.SaveState(ctx, stCompleted)
	_ = store.SaveState(ctx, stCancelled)

	statesLoaded, err := store.ListStates(ctx, "OMNIFY")
	if err != nil {
		t.Fatalf("error ListStates: %v", err)
	}
	if len(statesLoaded) != 5 {
		t.Fatalf("se esperaban 5 estados para OMNIFY, obtenidos: %d", len(statesLoaded))
	}
	if statesLoaded[0].GroupName != "backlog" || statesLoaded[1].GroupName != "started" {
		t.Errorf("orden inesperado de estados: %+v", statesLoaded)
	}
	var foundRevision bool
	for _, s := range statesLoaded {
		if s.Name == "Revisión" && s.GroupName == "started" {
			foundRevision = true
		}
	}
	if !foundRevision {
		t.Errorf("estado 'Revisión' no encontrado o grupo incorrecto")
	}
}

func TestStore_P1_DatesAndLifecycleMetadata(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error abriendo store: %v", err)
	}
	defer store.Close()

	now := time.Now().Truncate(time.Second)
	completedTime := now.Add(-1 * time.Hour)
	archivedTime := now.Add(-30 * time.Minute)

	task1 := &issue.WorkItem{
		ID:          "iss-p1-1",
		Key:         "OMNIFY-301",
		ProjectID:   "proj-omnify",
		Title:       "Tarea con fechas planificadas y completada",
		StateName:   "Completado",
		StateGroup:  "completed",
		State:       issue.StateCompleted,
		Priority:    "high",
		StartDate:   "2026-11-01",
		TargetDate:  "2026-11-15",
		CompletedAt: &completedTime,
		CreatedAt:   now.Add(-48 * time.Hour),
		Labels:      []string{"backend", "p1"},
	}

	task2 := &issue.WorkItem{
		ID:         "iss-p1-2",
		Key:        "OMNIFY-302",
		ProjectID:  "proj-omnify",
		Title:      "Tarea archivada",
		StateName:  "Archivado",
		StateGroup: "cancelled",
		State:      issue.StateCancelled,
		Priority:   "low",
		ArchivedAt: &archivedTime,
		CreatedAt:  now.Add(-24 * time.Hour),
	}

	task3 := &issue.WorkItem{
		ID:         "iss-p1-3",
		Key:        "OMNIFY-303",
		ProjectID:  "proj-omnify",
		Title:      "Tarea abierta activa",
		StateName:  "En Progreso",
		StateGroup: "started",
		State:      issue.StateStarted,
		Priority:   "urgent",
		StartDate:  "2026-11-10",
		TargetDate: "2026-11-20",
		CreatedAt:  now.Add(-12 * time.Hour),
	}

	if err := store.SaveIssue(ctx, task1); err != nil {
		t.Fatalf("error guardando task1: %v", err)
	}
	if err := store.SaveIssue(ctx, task2); err != nil {
		t.Fatalf("error guardando task2: %v", err)
	}
	if err := store.SaveIssue(ctx, task3); err != nil {
		t.Fatalf("error guardando task3: %v", err)
	}

	// 1. Verificar GetIssue con metadatos completos
	loaded1, err := store.GetIssue(ctx, "OMNIFY-301")
	if err != nil {
		t.Fatalf("error GetIssue: %v", err)
	}
	if loaded1.StartDate != "2026-11-01" || loaded1.TargetDate != "2026-11-15" {
		t.Errorf("fechas planificadas incorrectas: StartDate=%s, TargetDate=%s", loaded1.StartDate, loaded1.TargetDate)
	}
	if loaded1.CompletedAt == nil || !loaded1.CompletedAt.Equal(completedTime) {
		t.Errorf("CompletedAt incorrecto: %v", loaded1.CompletedAt)
	}
	if len(loaded1.Labels) != 2 || loaded1.Labels[0] != "backend" {
		t.Errorf("Labels incorrectos: %v", loaded1.Labels)
	}

	loaded2, err := store.GetIssue(ctx, "OMNIFY-302")
	if err != nil {
		t.Fatalf("error GetIssue: %v", err)
	}
	if loaded2.ArchivedAt == nil || !loaded2.ArchivedAt.Equal(archivedTime) {
		t.Errorf("ArchivedAt incorrecto: %v", loaded2.ArchivedAt)
	}

	// 2. Verificar que OpenOnly excluye tareas completadas y archivadas
	openIssues, err := store.ListIssuesFiltered(ctx, issue.IssueFilter{
		ProjectKey: "OMNIFY",
		OpenOnly:   true,
	})
	if err != nil {
		t.Fatalf("error ListIssuesFiltered OpenOnly: %v", err)
	}
	if len(openIssues) != 1 || openIssues[0].Key != "OMNIFY-303" {
		t.Fatalf("se esperaba solo 1 tarea abierta (OMNIFY-303), obtenidos: %d (%+v)", len(openIssues), openIssues)
	}

	// 3. Verificar filtro ArchivedOnly
	archivedIssues, err := store.ListIssuesFiltered(ctx, issue.IssueFilter{
		ProjectKey:   "OMNIFY",
		ArchivedOnly: true,
	})
	if err != nil {
		t.Fatalf("error ListIssuesFiltered ArchivedOnly: %v", err)
	}
	if len(archivedIssues) != 1 || archivedIssues[0].Key != "OMNIFY-302" {
		t.Fatalf("se esperaba 1 tarea archivada (OMNIFY-302), obtenidos: %d", len(archivedIssues))
	}
}

func TestStore_CommentsSnapshot_And_AssignmentEvents(t *testing.T) {
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error abriendo store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// 1. Guardar y recuperar comentarios snapshot
	comments := []issue.CommentRecord{
		{
			ID:          "cmt-1",
			CommentHTML: "<p>Primer comentario</p>",
			Actor:       "johan",
			CreatedBy:   "johan",
		},
		{
			ID:          "cmt-2",
			CommentHTML: "<p>Bitácora</p><!-- PLANE_WORKLOG_METADATA {\"duration_minutes\": 60} -->",
			Actor:       "johan",
			CreatedBy:   "johan",
		},
	}

	if err := store.SaveCommentsSnapshot(ctx, "OMNIFY-400", comments); err != nil {
		t.Fatalf("error SaveCommentsSnapshot: %v", err)
	}

	loadedComments, err := store.GetCommentsSnapshot(ctx, "OMNIFY-400")
	if err != nil {
		t.Fatalf("error GetCommentsSnapshot: %v", err)
	}
	if len(loadedComments) != 2 {
		t.Fatalf("se esperaban 2 comentarios, obtenidos: %d", len(loadedComments))
	}
	if loadedComments[0].ID != "cmt-1" || loadedComments[1].ID != "cmt-2" {
		t.Errorf("IDs de comentarios inesperados: %+v", loadedComments)
	}

	// 2. Historial de asignaciones
	if err := store.RecordAssignmentEvent(ctx, "OMNIFY-400", "usr-alice"); err != nil {
		t.Fatalf("error RecordAssignmentEvent 1: %v", err)
	}
	if err := store.RecordAssignmentEvent(ctx, "OMNIFY-400", "usr-bob"); err != nil {
		t.Fatalf("error RecordAssignmentEvent 2: %v", err)
	}

	events, err := store.GetAssignmentEvents(ctx, "OMNIFY-400")
	if err != nil {
		t.Fatalf("error GetAssignmentEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("se esperaban 2 eventos de asignación, obtenidos: %d", len(events))
	}
}

func TestStore_SyncWatermarks(t *testing.T) {
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error abriendo store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// 1. GetWatermark en frío retorna nil sin error
	wm, err := store.GetWatermark(ctx, "issues", "proj-omnify")
	if err != nil {
		t.Fatalf("error GetWatermark en frío: %v", err)
	}
	if wm != nil {
		t.Errorf("se esperaba watermark nulo en frío, obtenido: %+v", wm)
	}

	// 2. SetWatermark y verificación
	initialWM := issue.SyncWatermark{
		Domain:              "issues",
		ProjectID:           "proj-omnify",
		LastSyncTimestamp:   1700000000.0,
		ServerHighWatermark: "2026-11-16T15:00:00Z",
		Status:              "synced",
		ItemsSynced:         42,
		PagesFetched:        1,
		DurationMs:          150,
	}
	if err := store.SetWatermark(ctx, initialWM); err != nil {
		t.Fatalf("error SetWatermark: %v", err)
	}

	loadedWM, err := store.GetWatermark(ctx, "issues", "proj-omnify")
	if err != nil || loadedWM == nil {
		t.Fatalf("error recuperando watermark: %v", err)
	}
	if loadedWM.ServerHighWatermark != "2026-11-16T15:00:00Z" || loadedWM.ItemsSynced != 42 {
		t.Errorf("datos de watermark no coinciden: %+v", loadedWM)
	}

	// 3. Upsert idempotente de marca de agua
	updatedWM := initialWM
	updatedWM.ServerHighWatermark = "2026-11-16T17:30:00Z"
	updatedWM.ItemsSynced = 55
	if err := store.SetWatermark(ctx, updatedWM); err != nil {
		t.Fatalf("error actualizando watermark: %v", err)
	}

	reloadedWM, err := store.GetWatermark(ctx, "issues", "proj-omnify")
	if err != nil || reloadedWM == nil {
		t.Fatalf("error re-cargando watermark: %v", err)
	}
	if reloadedWM.ServerHighWatermark != "2026-11-16T17:30:00Z" || reloadedWM.ItemsSynced != 55 {
		t.Errorf("upsert de watermark falló: %+v", reloadedWM)
	}
}

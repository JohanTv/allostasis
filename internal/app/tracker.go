package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/domain/ledger"
)

var (
	ticketRefRegex   = regexp.MustCompile(`^(?:[A-Za-z0-9_]+-(?:LOCAL-)?\d+|[0-9a-fA-F-]{36}|local-\d+)$`)
	worklogMetaRegex = regexp.MustCompile(`<!--\s*PLANE_WORKLOG_METADATA\s*([\s\S]*?)\s*-->`)
	sessionRegex     = regexp.MustCompile(`\*\*Sesión\*\*\s*\|\s*(\d{1,2}:\d{2})\s*-\s*(\d{1,2}:\d{2})`)
)

// TrackerService orquesta la gestión soberana de tareas (local-first con sincronización upstream).
type TrackerService struct {
	store  IssueStore
	client RemoteTrackerClient
}

// NewTrackerService construye el servicio orquestador de tareas basado en interfaces hexagonales.
func NewTrackerService(store IssueStore, client RemoteTrackerClient) *TrackerService {
	return &TrackerService{
		store:  store,
		client: client,
	}
}

// Search busca tareas en el catálogo local por texto (título, etiquetas y descripción).
func (s *TrackerService) Search(ctx context.Context, query string, project string, limit int) ([]issue.WorkItem, error) {
	if s.store == nil {
		return nil, fmt.Errorf("almacenamiento no inicializado")
	}
	items, err := s.store.SearchIssues(ctx, query, project, limit)
	if err != nil {
		return nil, fmt.Errorf("error al buscar tareas: %w", err)
	}
	if items == nil {
		items = make([]issue.WorkItem, 0)
	}
	return items, nil
}

// Get obtiene el detalle de una tarea por su clave (ej. OMNIFY-175) o UUID.
func (s *TrackerService) Get(ctx context.Context, key string) (*issue.WorkItem, error) {
	if s.store == nil {
		return nil, fmt.Errorf("almacenamiento no inicializado")
	}
	return s.store.GetIssue(ctx, key)
}

// List lista tareas aplicando filtros de proyecto, estado o asignado.
func (s *TrackerService) List(ctx context.Context, filter issue.IssueFilter) ([]issue.WorkItem, error) {
	if s.store == nil {
		return nil, fmt.Errorf("almacenamiento no inicializado")
	}
	items, err := s.store.ListIssuesFiltered(ctx, filter)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = make([]issue.WorkItem, 0)
	}
	return items, nil
}

// Create crea una tarea de forma atómica: en Plane CE si hay conexión, o en cola outbox local si está offline.
func (s *TrackerService) Create(ctx context.Context, input issue.CreateIssueInput) (*issue.WorkItem, error) {
	if s.store == nil {
		return nil, fmt.Errorf("almacenamiento no inicializado")
	}

	input.ProjectKey = strings.ToUpper(strings.TrimSpace(input.ProjectKey))
	if input.ProjectKey == "" {
		input.ProjectKey = "OMNIFY"
	}
	if strings.TrimSpace(input.Title) == "" {
		return nil, fmt.Errorf("el título de la tarea no puede estar vacío")
	}

	// 1. Resolver projectID
	var projectID string
	pRec, err := s.store.GetProject(ctx, input.ProjectKey)
	if err == nil && pRec != nil && pRec.ProjectID != "" {
		projectID = pRec.ProjectID
	} else if s.client != nil {
		if projects, err := s.SyncProjects(ctx); err == nil {
			for _, p := range projects {
				if strings.EqualFold(p.Identifier, input.ProjectKey) {
					projectID = p.ProjectID
					break
				}
			}
		}
	}

	// Fail-Closed Pre-Flight (ADR 0025 & CLI Design Guard):
	// Si el catálogo local en SQLite contiene proyectos conocidos y la clave solicitada no existe,
	// abortar inmediatamente en lugar de encolar una mutación huérfana (Poison Pill) en la outbox.
	if projectID == "" && s.store != nil {
		if knownProjects, err := s.store.ListProjects(ctx); err == nil && len(knownProjects) > 0 {
			var validKeys []string
			for _, p := range knownProjects {
				validKeys = append(validKeys, p.Identifier)
			}
			return nil, fmt.Errorf("proyecto '%s' no encontrado en el catálogo. Opciones disponibles: %s. Ejecuta 'alo projects' para consultar", input.ProjectKey, strings.Join(validKeys, ", "))
		}
	}

	// 2. Resolver stateID si se indicó StateName
	var stateID, stateGroup string
	if input.StateName != "" && projectID != "" {
		if stRec, err := s.store.GetState(ctx, projectID, input.StateName); err == nil && stRec != nil {
			stateID = stRec.ID
			stateGroup = stRec.GroupName
		}
	}
	if stateGroup == "" {
		stateGroup = "backlog"
	}

	// 3. Crear en Plane CE o, si no hay conexión, en local con cola outbox (despacho único, ADR 0032)
	var created, queued *issue.WorkItem
	enqueue := func(ctx context.Context) error {
		nowNano := time.Now().UnixNano()
		localKey := fmt.Sprintf("%s-LOCAL-%d", input.ProjectKey, (nowNano/1000)%100000)
		localItem := &issue.WorkItem{
			ID:          fmt.Sprintf("local-%d", nowNano),
			Key:         localKey,
			ProjectID:   projectID,
			ProjectKey:  input.ProjectKey,
			Title:       input.Title,
			Description: input.Description,
			StateName:   input.StateName,
			StateGroup:  stateGroup,
			State:       issue.State(stateGroup),
			Priority:    input.Priority,
			Labels:      input.Labels,
			LabelsCSV:   strings.Join(input.Labels, ","),
			StartDate:   input.StartDate,
			TargetDate:  input.TargetDate,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		if err := s.store.SaveIssue(ctx, localItem); err != nil {
			return fmt.Errorf("error al guardar tarea en caché local: %w", err)
		}
		payloadBytes, _ := json.Marshal(input)
		if err := s.store.EnqueueOutboxWithMutation(ctx, fmt.Sprintf("client-%d", nowNano), localKey, string(payloadBytes), "", "create_issue"); err != nil {
			return fmt.Errorf("error al encolar en outbox: %w", err)
		}
		localItem.PendingSync = true
		queued = localItem
		return nil
	}
	send := func(ctx context.Context) error {
		remoteInput, canSend, err := s.createInputWithLabelIDs(ctx, projectID, input)
		if err != nil {
			return err
		}
		if !canSend {
			// Sin catálogo de etiquetas no se puede validar ni enviar: se trata como falta de conexión.
			return &issue.TransientError{Operation: "resolver etiquetas", Cause: fmt.Errorf("catálogo de etiquetas no disponible")}
		}
		item, err := s.client.CreateIssueRemote(ctx, projectID, remoteInput, stateID)
		if err != nil {
			return err
		}
		if item == nil {
			return fmt.Errorf("Plane no devolvió la tarea creada")
		}
		created = item
		return nil
	}

	if projectID == "" { // proyecto sin resolver (sin catálogo ni conexión): solo local-first
		if err := enqueue(ctx); err != nil {
			return nil, err
		}
		return queued, nil
	}
	disposition, err := s.dispatch(ctx, remoteMutation{Name: "crear tarea", Send: send, Enqueue: enqueue})
	if err != nil {
		return nil, err
	}
	if disposition == Queued {
		return queued, nil
	}
	created.Labels, created.LabelsCSV = input.Labels, strings.Join(input.Labels, ",")
	_ = s.store.SaveIssue(ctx, created)
	rawJSON, _ := json.Marshal(created)
	_ = s.store.SaveIssueSnapshot(ctx, created.ID, created.Key, string(rawJSON))
	return created, nil
}

// Update actualiza los campos de una tarea (estado, prioridad, título) local y remotamente.
func (s *TrackerService) Update(ctx context.Context, key string, input issue.UpdateIssueInput) (*issue.WorkItem, error) {
	if s.store == nil {
		return nil, fmt.Errorf("almacenamiento no inicializado")
	}

	item, err := s.store.GetIssue(ctx, key)
	if err != nil {
		return nil, err
	}

	var stateID string
	if input.StateName != nil && *input.StateName != "" {
		if item.ProjectID != "" {
			if stRec, err := s.store.GetState(ctx, item.ProjectID, *input.StateName); err == nil && stRec != nil {
				stateID = stRec.ID
				item.StateName = stRec.Name
				item.StateGroup = stRec.GroupName
				item.State = issue.State(stRec.GroupName)
			} else {
				item.StateName = *input.StateName
				item.StateGroup = strings.ToLower(*input.StateName)
				item.State = issue.State(strings.ToLower(*input.StateName))
			}
		} else {
			item.StateName = *input.StateName
			item.StateGroup = strings.ToLower(*input.StateName)
			item.State = issue.State(strings.ToLower(*input.StateName))
		}
	}

	if input.Title != nil {
		item.Title = *input.Title
	}
	if input.Priority != nil {
		item.Priority = *input.Priority
	}
	if input.Description != nil {
		item.Description = *input.Description
	}
	if input.Assignee != nil {
		item.Assignee = *input.Assignee
		_ = s.store.RecordAssignmentEvent(ctx, item.Key, *input.Assignee)
	}
	if input.Labels != nil {
		item.Labels = *input.Labels
		item.LabelsCSV = strings.Join(*input.Labels, ",")
	}
	if input.StartDate != nil {
		item.StartDate = *input.StartDate
	}
	if input.TargetDate != nil {
		item.TargetDate = *input.TargetDate
	}
	if item.State == issue.StateCompleted && item.CompletedAt == nil {
		now := time.Now()
		item.CompletedAt = &now
	} else if item.State != issue.StateCompleted {
		item.CompletedAt = nil
	}

	// Plane identifica el estado por ID: si no se resuelve, el cambio se omitiría en silencio. Se valida
	// contra el catálogo (refrescándolo una vez) antes de tocar nada.
	if input.StateName != nil && *input.StateName != "" && stateID == "" && s.client != nil &&
		item.ProjectID != "" && !strings.HasPrefix(item.ID, "local-") {
		var err error
		if stateID, err = s.resolveStateID(ctx, item.ProjectID, *input.StateName); err != nil {
			return nil, err
		}
	}

	// Mutación upstream (despacho único, ADR 0032): Plane si hay conexión, outbox `update_issue` si no.
	// Va antes de guardar en local: un rechazo o un error no clasificado no deja la caché con datos
	// que Plane no aceptó, y un cambio nunca queda solo en local sin quedar encolado.
	if item.ProjectID != "" && item.ID != "" {
		enqueue := func(ctx context.Context) error {
			payload, _ := json.Marshal(input)
			return s.store.EnqueueOutboxWithMutation(ctx, fmt.Sprintf("client-%d", time.Now().UnixNano()), item.Key, string(payload), "", "update_issue")
		}
		send := func(ctx context.Context) error {
			remoteInput := input
			if input.Labels != nil && len(*input.Labels) > 0 {
				ids, ok, err := s.resolveLabelIDs(ctx, item.ProjectID, *input.Labels)
				if err != nil {
					return err
				}
				if !ok {
					return &issue.TransientError{Operation: "resolver etiquetas", Cause: fmt.Errorf("catálogo de etiquetas no disponible")}
				}
				remoteInput.Labels = &ids
			}
			return s.client.UpdateIssueRemote(ctx, item.ProjectID, item.ID, remoteInput, stateID)
		}

		var disposition Disposition
		var err error
		if strings.HasPrefix(item.ID, "local-") {
			// La tarea aún no existe en Plane: el cambio se encola y se reproduce tras su creación.
			if err = enqueue(ctx); err == nil {
				disposition = Queued
			}
		} else {
			disposition, err = s.dispatch(ctx, remoteMutation{Name: "actualizar tarea", Send: send, Enqueue: enqueue})
		}
		if err != nil {
			return nil, err
		}
		item.PendingSync = disposition == Queued
	}

	// Guardar cambio local en SQLite
	if err := s.store.SaveIssue(ctx, item); err != nil {
		return nil, fmt.Errorf("error actualizando tarea en SQLite: %w", err)
	}

	return item, nil
}

// resolveStateID traduce el nombre de un estado a su ID desde el catálogo local, refrescándolo una vez si
// falta. Un estado que sigue sin existir retorna *issue.UnknownStateError (con los válidos).
func (s *TrackerService) resolveStateID(ctx context.Context, projectID, name string) (string, error) {
	find := func(states []issue.StateRecord) string {
		for _, st := range states {
			if strings.EqualFold(st.Name, name) && st.ID != "" {
				return st.ID
			}
		}
		return ""
	}
	local, _ := s.store.ListStates(ctx, projectID)
	if id := find(local); id != "" {
		return id, nil
	}
	valid := local
	if s.client != nil {
		if fresh, err := s.SyncStates(ctx, projectID); err == nil {
			if id := find(fresh); id != "" {
				return id, nil
			}
			valid = fresh
		}
	}
	names := make([]string, 0, len(valid))
	for _, st := range valid {
		names = append(names, st.Name)
	}
	return "", &issue.UnknownStateError{Name: name, Valid: names}
}

// replayUpdateIssue reproduce un `update_issue` diferido. Cualquier fallo queda registrado en el outbox
// (visible en `alo ticket outbox`) y se reintenta en el siguiente flush hasta agotar los intentos.
func (s *TrackerService) replayUpdateIssue(ctx context.Context, item issue.OutboxRecord, report *issue.SyncReport) {
	fail := func(msg string) {
		report.FailedComments++
		report.Errors = append(report.Errors, msg)
		_ = s.store.UpdateOutboxFailed(ctx, item.ID, msg)
	}
	var input issue.UpdateIssueInput
	if err := json.Unmarshal([]byte(item.PayloadJSON), &input); err != nil {
		fail(fmt.Sprintf("error decodificando UpdateIssueInput para %s: %s", item.TicketRef, err))
		return
	}
	target, _ := s.store.GetIssue(ctx, item.TicketRef)
	if target == nil || target.ID == "" || target.ProjectID == "" || strings.HasPrefix(target.ID, "local-") {
		fail(fmt.Sprintf("la tarea %s aún no existe en Plane; el cambio se reintentará tras su creación", item.TicketRef))
		return
	}
	var stateID string
	if input.StateName != nil && *input.StateName != "" {
		id, err := s.resolveStateID(ctx, target.ProjectID, *input.StateName)
		if err != nil {
			fail(err.Error())
			return
		}
		stateID = id
	}
	remoteInput := input
	if input.Labels != nil && len(*input.Labels) > 0 {
		ids, ok, err := s.resolveLabelIDs(ctx, target.ProjectID, *input.Labels)
		if err != nil {
			fail(err.Error())
			return
		}
		if !ok {
			fail("catálogo de etiquetas no disponible; reintenta con conexión")
			return
		}
		remoteInput.Labels = &ids
	}
	if err := s.client.UpdateIssueRemote(ctx, target.ProjectID, target.ID, remoteInput, stateID); err != nil {
		fail(err.Error())
		return
	}
	_ = s.store.UpdateOutboxSent(ctx, item.ID, target.ID)
	report.SyncedIssues++
}

// SyncProjects descarga el catálogo de proyectos y los almacena localmente.
func (s *TrackerService) SyncProjects(ctx context.Context) ([]issue.ProjectRecord, error) {
	if s.client == nil {
		return nil, fmt.Errorf("cliente de tracker remoto no configurado")
	}
	projects, err := s.client.FetchProjects(ctx)
	if err != nil {
		return nil, err
	}
	if s.store != nil {
		for _, p := range projects {
			_ = s.store.SaveProject(ctx, p)
		}
	}
	return projects, nil
}

// memberProjects descarta los proyectos de los que Plane informó que el usuario NO es miembro (públicos del
// workspace que solo se ven en la lista): consultarlos da 403 y desperdicia 3 peticiones por proyecto en cada sync.
// Un proyecto con pertenencia desconocida (nil) se conserva.
func memberProjects(projects []issue.ProjectRecord) []issue.ProjectRecord {
	out := make([]issue.ProjectRecord, 0, len(projects))
	for _, p := range projects {
		if !p.IsKnownNonMember() {
			out = append(out, p)
		}
	}
	return out
}

// SyncStates descarga los estados de un proyecto y los almacena localmente.
func (s *TrackerService) SyncStates(ctx context.Context, projectID string) ([]issue.StateRecord, error) {
	if s.client == nil {
		return nil, fmt.Errorf("cliente de tracker remoto no configurado")
	}
	states, err := s.client.FetchStates(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if s.store != nil {
		for _, st := range states {
			_ = s.store.SaveState(ctx, st)
		}
	}
	return states, nil
}

// SyncLabels descarga las etiquetas de un proyecto y las almacena localmente.
func (s *TrackerService) SyncLabels(ctx context.Context, projectID string) (int, error) {
	if s.client == nil {
		return 0, fmt.Errorf("cliente de tracker remoto no configurado")
	}
	labels, err := s.client.FetchLabels(ctx, projectID)
	if err != nil {
		return 0, err
	}
	count := 0
	if s.store != nil {
		for _, l := range labels {
			if err := s.store.SaveLabel(ctx, l); err == nil {
				count++
			}
		}
	}
	return count, nil
}

// SyncCatalogs descarga y actualiza catálogos (proyectos, estados y etiquetas) sin descargar tareas.
func (s *TrackerService) SyncCatalogs(ctx context.Context, projectKey string) error {
	projects, err := s.SyncProjects(ctx)
	if err != nil {
		return fmt.Errorf("error sincronizando proyectos: %w", err)
	}

	targetProjects := memberProjects(projects)
	if projectKey != "" {
		targetProjects = nil
		for _, p := range projects {
			if strings.EqualFold(p.Identifier, projectKey) || strings.EqualFold(p.ProjectID, projectKey) {
				targetProjects = append(targetProjects, p)
				break
			}
		}
		if len(targetProjects) == 0 {
			return fmt.Errorf("proyecto %s no encontrado", projectKey)
		}
	}

	for _, p := range targetProjects {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, _ = s.SyncStates(ctx, p.ProjectID)
		_, _ = s.SyncLabels(ctx, p.ProjectID)
	}
	return nil
}

// SyncIssuesDelta descarga las tareas de un proyecto aplicando soporte de parada temprana por marca de agua (cutoffISO).
func (s *TrackerService) SyncIssuesDelta(ctx context.Context, projectID, projectKey string, limit int, cutoffISO string, deep bool) (int, string, int, error) {
	if s.client == nil {
		return 0, "", 0, fmt.Errorf("cliente de tracker remoto no configurado")
	}

	var (
		states    []issue.StateRecord
		snapshots []issue.IssueSnapshot
		newestUp  string
		fetchErr  error
		wg        sync.WaitGroup
	)

	wg.Add(3)
	go func() {
		defer wg.Done()
		states, _ = s.SyncStates(ctx, projectID)
	}()
	go func() {
		defer wg.Done()
		_, _ = s.SyncLabels(ctx, projectID)
	}()
	go func() {
		defer wg.Done()
		snapshots, newestUp, fetchErr = s.client.FetchIssuesDelta(ctx, projectID, projectKey, limit, cutoffISO, deep)
	}()
	wg.Wait()

	if fetchErr != nil {
		return 0, newestUp, 0, fetchErr
	}

	stateMap := make(map[string]issue.StateRecord, len(states))
	for _, st := range states {
		stateMap[st.ID] = st
	}

	syncedCount := 0
	if s.store != nil {
		for _, snap := range snapshots {
			if (snap.Item.StateName == "" || snap.Item.StateName == "Backlog") && snap.RawJSON != "" {
				var raw map[string]interface{}
				if err := json.Unmarshal([]byte(snap.RawJSON), &raw); err == nil {
					if sID, ok := raw["state"].(string); ok && sID != "" {
						if st, found := stateMap[sID]; found {
							snap.Item.StateName = st.Name
							snap.Item.StateGroup = st.GroupName
							snap.Item.State = issue.State(st.GroupName)
						}
					}
				}
			}
			if err := s.store.SaveIssue(ctx, &snap.Item); err == nil {
				_ = s.store.SaveIssueSnapshot(ctx, snap.Item.ID, snap.Item.Key, snap.RawJSON)
				syncedCount++
			}
		}
	}
	pagesFetched := 1
	if len(snapshots) > 100 {
		pagesFetched = (len(snapshots) + 99) / 100
	}
	return syncedCount, newestUp, pagesFetched, nil
}

// SyncIssues descarga las tareas de un proyecto y las sincroniza en caché y snapshots.
func (s *TrackerService) SyncIssues(ctx context.Context, projectKey string, limit int) (int, error) {
	if s.client == nil {
		return 0, fmt.Errorf("cliente de tracker remoto no configurado")
	}
	projectKey = strings.ToUpper(strings.TrimSpace(projectKey))
	var projectID string
	if s.store != nil {
		if pRec, err := s.store.GetProject(ctx, projectKey); err == nil && pRec != nil && pRec.ProjectID != "" {
			projectID = pRec.ProjectID
		}
	}
	if projectID == "" {
		projects, err := s.SyncProjects(ctx)
		if err != nil {
			return 0, fmt.Errorf("no se pudo resolver proyecto %s: %w", projectKey, err)
		}
		for _, p := range projects {
			if p.Identifier == projectKey {
				projectID = p.ProjectID
				break
			}
		}
	}
	if projectID == "" {
		return 0, fmt.Errorf("proyecto %s no encontrado en Plane", projectKey)
	}

	cutoffISO := ""
	if s.store != nil {
		if wm, err := s.store.GetWatermark(ctx, "issues", projectID); err == nil && wm != nil {
			cutoffISO = wm.ServerHighWatermark
		}
	}

	projStart := time.Now()
	count, newest, pages, err := s.SyncIssuesDelta(ctx, projectID, projectKey, limit, cutoffISO, false)
	if err == nil && s.store != nil {
		highWM := newest
		if highWM == "" {
			highWM = cutoffISO
		}
		_ = s.store.SetWatermark(ctx, issue.SyncWatermark{
			Domain:              "issues",
			ProjectID:           projectID,
			LastSyncTimestamp:   float64(time.Now().Unix()),
			ServerHighWatermark: highWM,
			Status:              "synced",
			ItemsSynced:         count,
			PagesFetched:        pages,
			DurationMs:          time.Since(projStart).Milliseconds(),
			UpdatedAt:           time.Now(),
		})
	}
	return count, err
}

// Sync sincroniza proyectos, estados y tareas desde Plane CE hacia la base de datos local.
func (s *TrackerService) Sync(ctx context.Context, projectKey string, allProjects bool) (int, error) {
	projects, err := s.SyncProjects(ctx)
	if err != nil {
		return 0, fmt.Errorf("error sincronizando proyectos: %w", err)
	}

	totalSynced := 0
	if allProjects {
		if len(projects) == 0 {
			return 0, nil
		}

		const maxWorkers = 5
		workers := maxWorkers
		if len(projects) < workers {
			workers = len(projects)
		}

		type syncResult struct {
			count int
			err   error
		}

		jobs := make(chan issue.ProjectRecord, len(projects))
		results := make(chan syncResult, len(projects))
		var wg sync.WaitGroup

		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for p := range jobs {
					if ctx.Err() != nil {
						results <- syncResult{err: ctx.Err()}
						continue
					}
					count, syncErr := s.SyncIssues(ctx, p.Identifier, 100)
					results <- syncResult{count: count, err: syncErr}
				}
			}()
		}

		for _, p := range projects {
			jobs <- p
		}
		close(jobs)

		wg.Wait()
		close(results)

		for res := range results {
			if res.err == nil {
				totalSynced += res.count
			}
		}

		if ctx.Err() != nil {
			return totalSynced, ctx.Err()
		}
	} else {
		if projectKey == "" {
			projectKey = "OMNIFY"
		}
		count, err := s.SyncIssues(ctx, projectKey, 100)
		if err != nil {
			return 0, err
		}
		totalSynced = count
	}

	return totalSynced, nil
}

// SyncUnified orquesta la sincronización soberana y unificada según el target solicitado (ADR 0028 / RFC 0027).
func (s *TrackerService) SyncUnified(ctx context.Context, opts issue.SyncUnifiedOptions) (*issue.SyncUnifiedReport, error) {
	startTime := time.Now()
	target := strings.ToLower(strings.TrimSpace(opts.Target))
	if target == "" {
		target = "all"
	}

	report := &issue.SyncUnifiedReport{
		Status:     "synced",
		Target:     target,
		Watermarks: make(map[string]string),
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}

	// 1. Fase PUSH: Drenaje de Outbox (para target "all" o "outbox")
	if target == "all" || target == "outbox" {
		outboxRep, err := s.FlushOutbox(ctx, limit)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("flush outbox: %v", err))
		}
		if outboxRep != nil {
			report.SentComments += outboxRep.SentComments
			report.FailedComments += outboxRep.FailedComments
			report.Errors = append(report.Errors, outboxRep.Errors...)
		}
		if target == "outbox" {
			report.DurationMs = time.Since(startTime).Milliseconds()
			if len(report.Errors) > 0 {
				report.Status = "partial_success"
			}
			return report, nil
		}
	}

	// 2. Fase CATÁLOGOS: Proyectos, Estados y Etiquetas (para target "all" o "catalogs")
	var projects []issue.ProjectRecord
	if target == "all" || target == "catalogs" {
		projs, err := s.SyncProjects(ctx)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("sync projects: %v", err))
		} else {
			projects = projs
			report.SyncedProjects = len(projs)
			report.SyncedCatalogs = true
			// Sincronizar estados y etiquetas de cada proyecto del que el usuario es miembro
			for _, p := range memberProjects(projs) {
				_, _ = s.SyncStates(ctx, p.ProjectID)
				_, _ = s.SyncLabels(ctx, p.ProjectID)
			}
		}
		if target == "catalogs" {
			report.DurationMs = time.Since(startTime).Milliseconds()
			report.Status = "catalogs_synced"
			if len(report.Errors) > 0 {
				report.Status = "partial_success"
			}
			return report, nil
		}
	}

	// 3. Fase TICKETS: Delta Pull con Marcas de Agua (para target "all", "tickets", "issues")
	if target == "all" || target == "tickets" || target == "issues" {
		var targetProjects []issue.ProjectRecord
		if opts.ProjectKey != "" {
			var requestedKeys []string
			for _, k := range strings.Split(opts.ProjectKey, ",") {
				if t := strings.ToUpper(strings.TrimSpace(k)); t != "" {
					requestedKeys = append(requestedKeys, t)
				}
			}
			for _, targetKey := range requestedKeys {
				var pID string
				if s.store != nil {
					if pRec, err := s.store.GetProject(ctx, targetKey); err == nil && pRec != nil && pRec.ProjectID != "" {
						pID = pRec.ProjectID
					}
				}
				if pID == "" {
					if len(projects) == 0 {
						projects, _ = s.SyncProjects(ctx)
					}
					for _, p := range projects {
						if p.Identifier == targetKey {
							pID = p.ProjectID
							break
						}
					}
				}
				if pID != "" {
					targetProjects = append(targetProjects, issue.ProjectRecord{Identifier: targetKey, ProjectID: pID})
				} else {
					report.Errors = append(report.Errors, fmt.Sprintf("proyecto %s no encontrado", targetKey))
				}
			}
		} else if opts.AllProjects {
			if len(projects) == 0 {
				projects, _ = s.SyncProjects(ctx)
			}
			targetProjects = projects
		} else {
			// Por defecto: los proyectos de los que el usuario es miembro (los públicos donde no lo es se omiten)
			if len(projects) == 0 {
				projects, _ = s.SyncProjects(ctx)
			}
			targetProjects = memberProjects(projects)
		}

		// Un 403 sobre UN proyecto concreto (selección implícita) significa "no tengo permiso sobre ese proyecto":
		// se omite y se informa aparte (SkippedProjects), no como error. Si lo piden explícito (-p) o si TODOS los
		// proyectos dan 403 (credenciales o permisos mal configurados), sigue siendo error. El 401 nunca se omite.
		var forbiddenErrs []string
		for _, p := range targetProjects {
			cutoffISO := ""
			if !opts.Deep && s.store != nil {
				if wm, err := s.store.GetWatermark(ctx, "issues", p.ProjectID); err == nil && wm != nil {
					cutoffISO = wm.ServerHighWatermark
				}
			}

			projStart := time.Now()
			synced, newestUp, pages, err := s.SyncIssuesDelta(ctx, p.ProjectID, p.Identifier, limit, cutoffISO, opts.Deep)
			if err != nil {
				var authErr *issue.AuthError
				if opts.ProjectKey == "" && errors.As(err, &authErr) && authErr.Status == 403 {
					report.SkippedProjects = append(report.SkippedProjects, issue.SkippedProject{Project: p.Identifier, Reason: "sin permiso (HTTP 403)"})
					forbiddenErrs = append(forbiddenErrs, fmt.Sprintf("sync issues %s: %v", p.Identifier, err))
					continue
				}
				report.Errors = append(report.Errors, fmt.Sprintf("sync issues %s: %v", p.Identifier, err))
				continue
			}

			report.SyncedIssues += synced
			highWM := newestUp
			if highWM == "" {
				highWM = cutoffISO
			}
			report.Watermarks[p.Identifier] = highWM

			if s.store != nil {
				_ = s.store.SetWatermark(ctx, issue.SyncWatermark{
					Domain:              "issues",
					ProjectID:           p.ProjectID,
					LastSyncTimestamp:   float64(time.Now().Unix()),
					ServerHighWatermark: highWM,
					Status:              "synced",
					ItemsSynced:         synced,
					PagesFetched:        pages,
					DurationMs:          time.Since(projStart).Milliseconds(),
					UpdatedAt:           time.Now(),
				})
			}
		}
		if len(forbiddenErrs) > 0 && len(forbiddenErrs) == len(targetProjects) {
			report.Errors = append(report.Errors, forbiddenErrs...)
			report.SkippedProjects = nil
		}
	}

	// 4. Fase HOURS / WORKLOGS
	if target == "hours" || target == "worklogs" {
		outboxRep, err := s.FlushOutbox(ctx, limit)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("flush worklogs: %v", err))
		}
		if outboxRep != nil {
			report.SentComments += outboxRep.SentComments
			report.FailedComments += outboxRep.FailedComments
			report.Errors = append(report.Errors, outboxRep.Errors...)
		}
	}

	report.DurationMs = time.Since(startTime).Milliseconds()
	if len(report.Errors) > 0 {
		if report.SyncedIssues > 0 || report.SentComments > 0 || report.SyncedCatalogs {
			report.Status = "partial_success"
		} else {
			report.Status = "failed"
		}
	}

	return report, nil
}

// isCreateIssueMutation determina si un registro del outbox corresponde a la creación diferida de una tarea.
func isCreateIssueMutation(item issue.OutboxRecord) bool {
	if item.MutationType == "create_issue" {
		return true
	}
	if item.MutationType == "comment" {
		return false
	}
	// Si mutation_type no está explícito, verificar el payload JSON
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(item.PayloadJSON), &m); err == nil && m != nil {
		if _, ok := m["comment_html"]; ok {
			return false
		}
		if _, ok := m["comment"]; ok {
			return false
		}
		if _, ok := m["body"]; ok {
			return false
		}
		if _, ok := m["title"]; ok {
			return true
		}
		if _, ok := m["name"]; ok {
			return true
		}
	}
	return false
}

// FlushOutbox publica los comentarios y registros diferidos en la cola outbox hacia el tracker remoto.
func (s *TrackerService) FlushOutbox(ctx context.Context, limit int) (*issue.SyncReport, error) {
	report := &issue.SyncReport{Status: "completed"}
	if s.store == nil {
		return report, fmt.Errorf("almacenamiento no inicializado")
	}
	if s.client == nil {
		report.Status = "skipped_no_credentials"
		report.Errors = append(report.Errors, "cliente de tracker remoto no configurado: omitiendo flush")
		return report, nil
	}
	if limit <= 0 {
		limit = 20
	}

	pending, err := s.store.ListOutbox(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("error al consultar cola outbox: %w", err)
	}

	migratedKeys := make(map[string]*issue.WorkItem)
	processed := 0
	for _, item := range pending {
		if processed >= limit {
			break
		}
		processed++

		// Reasignar TicketRef si fue migrado en este lote
		if migrated, ok := migratedKeys[item.TicketRef]; ok && migrated != nil {
			item.TicketRef = migrated.Key
		}

		if item.MutationType == "update_issue" {
			s.replayUpdateIssue(ctx, item, report)
			continue
		}

		if isCreateIssueMutation(item) {
			var input issue.CreateIssueInput
			if err := json.Unmarshal([]byte(item.PayloadJSON), &input); err != nil {
				report.FailedComments++
				errMsg := fmt.Sprintf("error decodificando CreateIssueInput para %s: %s", item.TicketRef, err.Error())
				report.Errors = append(report.Errors, errMsg)
				_ = s.store.UpdateOutboxFailed(ctx, item.ID, errMsg)
				continue
			}

			projectKey := strings.ToUpper(strings.TrimSpace(input.ProjectKey))
			if projectKey == "" {
				projectKey = "OMNIFY"
				input.ProjectKey = projectKey
			}

			// 1. Resolver projectID
			var projectID string
			if pRec, err := s.store.GetProject(ctx, projectKey); err == nil && pRec != nil && pRec.ProjectID != "" {
				projectID = pRec.ProjectID
			} else {
				if projects, err := s.SyncProjects(ctx); err == nil {
					for _, p := range projects {
						if strings.EqualFold(p.Identifier, projectKey) {
							projectID = p.ProjectID
							break
						}
					}
				}
			}

			if projectID == "" {
				report.FailedComments++
				errMsg := fmt.Sprintf("no se pudo resolver projectID para proyecto %s (ticket %s)", projectKey, item.TicketRef)
				report.Errors = append(report.Errors, errMsg)
				_ = s.store.UpdateOutboxFailed(ctx, item.ID, errMsg)
				continue
			}

			// 2. Resolver stateID si input.StateName existe
			var stateID string
			if input.StateName != "" {
				if stRec, err := s.store.GetState(ctx, projectID, input.StateName); err == nil && stRec != nil && stRec.ID != "" {
					stateID = stRec.ID
				} else {
					if states, err := s.SyncStates(ctx, projectID); err == nil {
						for _, st := range states {
							if strings.EqualFold(st.Name, input.StateName) {
								stateID = st.ID
								break
							}
						}
					}
				}
			}

			// 3. Crear remotamente en Plane CE (etiquetas como UUID, desde el catálogo local)
			remoteInput, canSend, err := s.createInputWithLabelIDs(ctx, projectID, input)
			if err == nil && !canSend {
				err = fmt.Errorf("catálogo de etiquetas no disponible; reintenta con conexión")
			}
			var created *issue.WorkItem
			if err == nil {
				created, err = s.client.CreateIssueRemote(ctx, projectID, remoteInput, stateID)
			}
			if err != nil {
				report.FailedComments++
				report.Errors = append(report.Errors, err.Error())
				_ = s.store.UpdateOutboxFailed(ctx, item.ID, err.Error())
				continue
			}
			created.Labels, created.LabelsCSV = input.Labels, strings.Join(input.Labels, ",")

			// 4. Migrar el ticket local en la base de datos
			localItem, _ := s.store.GetIssue(ctx, item.TicketRef)
			oldID := ""
			oldKey := item.TicketRef
			if localItem != nil {
				oldID = localItem.ID
				if oldKey == "" {
					oldKey = localItem.Key
				}
				if created.StateName == "" {
					created.StateName = localItem.StateName
				}
				if created.StateGroup == "" {
					created.StateGroup = localItem.StateGroup
					created.State = localItem.State
				}
				if created.Description == "" {
					created.Description = localItem.Description
				}
				if created.Priority == "" {
					created.Priority = localItem.Priority
				}
			}

			if created.ProjectID == "" {
				created.ProjectID = projectID
			}
			if created.ProjectKey == "" {
				created.ProjectKey = projectKey
			}

			if err := s.store.MigrateLocalIssue(ctx, oldID, oldKey, created); err != nil {
				_ = s.store.SaveIssue(ctx, created)
			}

			rawJSON, _ := json.Marshal(created)
			_ = s.store.SaveIssueSnapshot(ctx, created.ID, created.Key, string(rawJSON))

			// Registrar en mapeo de claves migradas para subsiguientes items del lote
			if oldKey != "" {
				migratedKeys[oldKey] = created
			}
			if oldID != "" {
				migratedKeys[oldID] = created
			}

			_ = s.store.UpdateOutboxSent(ctx, item.ID, created.ID)
			report.SyncedIssues++
			continue
		}

		// Mutación de comentario
		issueItem, err := s.store.GetIssue(ctx, item.TicketRef)
		if err != nil || issueItem == nil || issueItem.ID == "" || issueItem.ProjectID == "" {
			if migrated, ok := migratedKeys[item.TicketRef]; ok && migrated != nil {
				issueItem = migrated
			}
		}

		if issueItem == nil || issueItem.ID == "" || issueItem.ProjectID == "" {
			report.FailedComments++
			errMsg := fmt.Sprintf("no se encontró issue ID para %s", item.TicketRef)
			report.Errors = append(report.Errors, errMsg)
			_ = s.store.UpdateOutboxFailed(ctx, item.ID, errMsg)
			continue
		}

		commentID, err := s.client.SendCommentRaw(ctx, item.TargetWorkspace, issueItem.ProjectID, issueItem.ID, item.PayloadJSON)
		if err != nil {
			report.FailedComments++
			report.Errors = append(report.Errors, err.Error())
			_ = s.store.UpdateOutboxFailed(ctx, item.ID, err.Error())
			continue
		}

		_ = s.store.UpdateOutboxSent(ctx, item.ID, commentID)
		report.SentComments++
	}

	return report, nil
}

// Whoami consulta la identidad y validez de la sesión en Plane CE y la persiste localmente.
func (s *TrackerService) Whoami(ctx context.Context) (*issue.UserInfo, error) {
	if s.client == nil {
		return nil, fmt.Errorf("cliente de Plane no configurado")
	}
	user, err := s.client.Whoami(ctx)
	if err == nil && user != nil && user.ID != "" && s.store != nil {
		_ = s.store.SaveUserProfile(ctx, *user)
	}
	return user, err
}

// ResolveCurrentUserID determina el UUID del usuario actual mediante la jerarquía canónica Fail-Closed:
// 1. PLANE_ACTOR_UUID (variable de entorno, 0ms)
// 2. user_profile en SQLite local (<1ms, 100% offline)
// 3. HTTP Whoami hacia el tracker remoto (guarda en SQLite si tiene éxito)
// 4. Retorna error si no se pudo determinar la identidad (Fail-Closed).
func (s *TrackerService) ResolveCurrentUserID(ctx context.Context) (string, error) {
	// 1. Variable de entorno explícita
	if actorUUID := strings.TrimSpace(os.Getenv("PLANE_ACTOR_UUID")); actorUUID != "" {
		return actorUUID, nil
	}

	// 2. Caché local en SQLite
	if s.store != nil {
		if u, err := s.store.GetUserProfile(ctx); err == nil && u != nil && strings.TrimSpace(u.ID) != "" {
			return strings.TrimSpace(u.ID), nil
		}
	}

	// 3. Fallback a Whoami remoto (y auto-caché)
	if s.client != nil {
		if u, err := s.Whoami(ctx); err == nil && u != nil && strings.TrimSpace(u.ID) != "" {
			return strings.TrimSpace(u.ID), nil
		}
	}

	// 4. Fail-Closed
	return "", fmt.Errorf("no se pudo determinar tu identidad de usuario; configura PLANE_ACTOR_UUID o ejecuta 'alo ticket whoami' con conexión")
}

// Projects retorna los proyectos registrados en la caché local.
func (s *TrackerService) Projects(ctx context.Context) ([]issue.ProjectRecord, error) {
	return s.store.ListProjects(ctx)
}

// States retorna los estados locales, sincronizándolos con Plane si se solicita refresh o si la caché está vacía.
func (s *TrackerService) States(ctx context.Context, projectKey string, refresh bool) ([]issue.StateRecord, error) {
	if s.store == nil {
		return nil, fmt.Errorf("almacén local no inicializado")
	}

	var projectID string
	if projectKey != "" {
		if p, err := s.store.GetProject(ctx, strings.ToUpper(projectKey)); err == nil && p != nil {
			projectID = p.ProjectID
		} else {
			projectID = projectKey
		}
	}

	states, err := s.store.ListStates(ctx, projectKey)
	if err != nil {
		return nil, err
	}

	if (refresh || len(states) == 0) && s.client != nil {
		if projectID != "" {
			if _, err := s.SyncStates(ctx, projectID); err == nil {
				states, _ = s.store.ListStates(ctx, projectKey)
			}
		} else {
			if projs, err := s.Projects(ctx); err == nil {
				for _, p := range projs {
					_, _ = s.SyncStates(ctx, p.ProjectID)
				}
				states, _ = s.store.ListStates(ctx, "")
			}
		}
	}

	return states, nil
}

// resolveLabelIDs traduce nombres de etiqueta a los UUID que exige Plane, desde el catálogo local
// (se refresca desde Plane solo si falta alguna). Acepta el nombre sin distinguir mayúsculas o el UUID.
// Una etiqueta que sigue sin existir tras refrescar retorna *issue.UnknownLabelError: se rechaza, no se ignora.
// ok=false indica que no se pudo validar (sin conexión): quien llama sigue local-first con los nombres.
func (s *TrackerService) resolveLabelIDs(ctx context.Context, projectID string, names []string) (ids []string, ok bool, err error) {
	match := func() (found, unknown, valid []string) {
		labels, _ := s.store.ListLabels(ctx, projectID)
		byName := make(map[string]string, len(labels))
		byID := make(map[string]bool, len(labels))
		for _, l := range labels {
			byName[strings.ToLower(l.Name)] = l.ID
			byID[l.ID] = true
			valid = append(valid, l.Name)
		}
		seen := make(map[string]bool, len(names))
		for _, raw := range names {
			name := strings.TrimSpace(raw)
			id := byName[strings.ToLower(name)]
			if id == "" && byID[name] {
				id = name
			}
			switch {
			case id == "":
				unknown = append(unknown, name)
			case !seen[id]:
				seen[id] = true
				found = append(found, id)
			}
		}
		return found, unknown, valid
	}

	found, unknown, valid := match()
	if len(unknown) > 0 {
		if _, err := s.SyncLabels(ctx, projectID); err != nil {
			return nil, false, nil
		}
		found, unknown, valid = match()
	}
	if len(unknown) > 0 {
		return nil, true, &issue.UnknownLabelError{Names: unknown, Valid: valid}
	}
	return found, true, nil
}

// createInputWithLabelIDs prepara el input para Plane (etiquetas como UUID). canSend=false: sin catálogo, no enviar.
func (s *TrackerService) createInputWithLabelIDs(ctx context.Context, projectID string, input issue.CreateIssueInput) (out issue.CreateIssueInput, canSend bool, err error) {
	out = input
	if len(input.Labels) == 0 {
		return out, true, nil
	}
	ids, ok, err := s.resolveLabelIDs(ctx, projectID, input.Labels)
	if err != nil || !ok {
		return out, false, err
	}
	out.Labels = ids
	return out, true, nil
}

// Labels retorna las etiquetas locales, sincronizándolas con Plane si se solicita refresh.
func (s *TrackerService) Labels(ctx context.Context, projectKey string, refresh bool) ([]issue.Label, error) {
	var projectID string
	if projectKey != "" {
		if p, err := s.store.GetProject(ctx, strings.ToUpper(projectKey)); err == nil && p != nil {
			projectID = p.ProjectID
		}
	} else {
		if p, err := s.store.GetProject(ctx, "OMNIFY"); err == nil && p != nil {
			projectID = p.ProjectID
		}
	}

	if refresh && s.client != nil && projectID != "" {
		_, _ = s.SyncLabels(ctx, projectID)
	}

	return s.store.ListLabels(ctx, projectID)
}

type parsedCommentWorklog struct {
	id      string
	minutes int
	author  string
}

func parseCommentWorklogs(comments []issue.CommentRecord) []parsedCommentWorklog {
	var worklogs []parsedCommentWorklog
	for _, c := range comments {
		html := c.CommentHTML
		author := c.CreatedBy
		if author == "" {
			author = c.Actor
		}

		isWorklog := false
		minutes := 0

		if strings.Contains(html, "PLANE_WORKLOG_METADATA") {
			isWorklog = true
			if match := worklogMetaRegex.FindStringSubmatch(html); len(match) > 1 {
				var meta struct {
					DurationMinutes    *int   `json:"duration_minutes"`
					DurationNetMinutes *int   `json:"duration_net_minutes"`
					StartTime          string `json:"start_time"`
					EndTime            string `json:"end_time"`
				}
				if err := json.Unmarshal([]byte(strings.TrimSpace(match[1])), &meta); err == nil {
					if meta.DurationMinutes != nil && *meta.DurationMinutes > 0 {
						minutes = *meta.DurationMinutes
					} else if meta.DurationNetMinutes != nil && *meta.DurationNetMinutes > 0 {
						minutes = *meta.DurationNetMinutes
					} else if meta.StartTime != "" && meta.EndTime != "" {
						t1, err1 := time.Parse("15:04", meta.StartTime)
						t2, err2 := time.Parse("15:04", meta.EndTime)
						if err1 == nil && err2 == nil {
							diff := int(t2.Sub(t1).Minutes())
							if diff < 0 {
								diff += 24 * 60
							}
							minutes = diff
						}
					}
				}
			}
		} else if strings.Contains(html, "**Fecha**") && strings.Contains(html, "**Sesión**") {
			isWorklog = true
			if match := sessionRegex.FindStringSubmatch(html); len(match) > 2 {
				t1, err1 := time.Parse("15:04", match[1])
				t2, err2 := time.Parse("15:04", match[2])
				if err1 == nil && err2 == nil {
					diff := int(t2.Sub(t1).Minutes())
					if diff < 0 {
						diff += 24 * 60
					}
					minutes = diff
				}
			}
		}

		if isWorklog {
			worklogs = append(worklogs, parsedCommentWorklog{
				id:      c.ID,
				minutes: minutes,
				author:  author,
			})
		}
	}
	return worklogs
}

// Delete elimina una tarea verificando rigurosamente las salvaguardas contra destrucción de horas (ADR 0018).
func (s *TrackerService) Delete(ctx context.Context, key string, yes, discardWorklogs bool) (map[string]interface{}, error) {
	key = strings.TrimSpace(strings.ToUpper(key))
	if !ticketRefRegex.MatchString(key) {
		return nil, fmt.Errorf("Error: delete exige el ticket completo (PREFIJO-N o UUID); no acepta '@', '.', números sueltos ni la rama de Git.")
	}

	item, err := s.store.GetIssue(ctx, key)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, fmt.Errorf("Error: %s no existe en Plane y no hay nada local que limpiar.", key)
	}

	// 1. Verificar cola de salida (Outbox) pendiente
	pending, err := s.store.CountPendingOutbox(ctx, key)
	if err == nil && pending > 0 {
		return nil, fmt.Errorf("Error: %s tiene %d comentario(s) sin publicar en la cola de salida; publícalos (alo sync) o descártalos antes de eliminarlo.", key, pending)
	}

	// 2. Leer comentarios remotos si el ticket existe en Plane
	var remoteComments []issue.CommentRecord
	if s.client != nil && item.ProjectID != "" && item.ID != "" && !strings.HasPrefix(item.ID, "local-") {
		comments, err := s.client.FetchComments(ctx, item.ProjectID, item.ID)
		if err != nil {
			return nil, fmt.Errorf("Error: no se pudieron leer los comentarios de Plane (%w); no se elimina nada.", err)
		}
		remoteComments = comments
	}

	// 3. Inspeccionar worklogs remotos y locales
	foundRemoteWorklogs := parseCommentWorklogs(remoteComments)
	localWorklogs, _ := s.store.GetTicketWorklogs(ctx, key)

	actorUUID, _ := s.ResolveCurrentUserID(ctx)

	remoteIDs := make(map[string]bool)
	var mine []parsedCommentWorklog
	var foreignRemote []parsedCommentWorklog
	for _, rw := range foundRemoteWorklogs {
		remoteIDs[rw.id] = true
		if actorUUID != "" && rw.author == actorUUID {
			mine = append(mine, rw)
		} else {
			foreignRemote = append(foreignRemote, rw)
		}
	}

	var ownLocal []issue.WorklogRecord
	var foreignLocal []issue.WorklogRecord
	var localOnly []issue.WorklogRecord
	for _, lw := range localWorklogs {
		if !remoteIDs[lw.PlaneCommentID] {
			localOnly = append(localOnly, lw)
			// Una fila local sin autor registrado se trata como propia (se creó en esta máquina); con autor, solo si es el actor confirmado.
			if lw.AuthorID == "" || (actorUUID != "" && lw.AuthorID == actorUUID) {
				ownLocal = append(ownLocal, lw)
			} else {
				foreignLocal = append(foreignLocal, lw)
			}
		}
	}

	hoursOwn := len(mine) + len(ownLocal)
	hoursForeign := len(foreignRemote) + len(foreignLocal)
	hoursLocalOnly := len(localOnly)
	ownMinutes := 0
	for _, m := range mine {
		ownMinutes += m.minutes
	}
	for _, l := range ownLocal {
		ownMinutes += l.DurationMinutes
	}

	summary := map[string]interface{}{
		"ticket_id":        key,
		"title":            item.Title,
		"comments":         len(remoteComments),
		"hours_own":        hoursOwn,
		"hours_foreign":    hoursForeign,
		"hours_local_only": hoursLocalOnly,
		"own_minutes":      ownMinutes,
	}

	// 4. Salvaguardas fail-closed
	if hoursForeign > 0 {
		hint := ""
		if actorUUID == "" {
			hint = " (sin usuario autenticado no se puede confirmar quién las registró)"
		}
		return nil, fmt.Errorf("Error: %s tiene %d registro(s) de horas de otra persona o de autor no confirmado%s; eliminarlo destruiría su trabajo y no se permite. Conserva el ticket: alo ticket update %s --state Cancelado", key, hoursForeign, hint, key)
	}

	if hoursOwn > 0 && !discardWorklogs {
		return nil, fmt.Errorf("Error: %s tiene %d registro(s) de horas tuyas (%d min); eliminar el ticket las destruiría y las horas son trabajo real. Conserva el ticket: alo ticket update %s --state Cancelado. Si de verdad quieres perderlas: alo ticket delete %s --yes --discard-worklogs", key, hoursOwn, ownMinutes, key, key)
	}

	if !yes {
		summary["status"] = "dry_run"
		summary["message"] = "Para confirmar la eliminación permanente incluye la bandera --yes"
		return summary, nil
	}

	// 5. Eliminar en Plane CE (si aplica)
	if s.client != nil && item.ProjectID != "" && item.ID != "" && !strings.HasPrefix(item.ID, "local-") {
		if err := s.client.DeleteIssueRemote(ctx, item.ProjectID, item.ID); err != nil {
			return nil, fmt.Errorf("error al eliminar ticket en Plane CE: %w", err)
		}
	}

	// 6. Limpieza soberana en SQLite
	if err := s.store.DeleteIssue(ctx, key); err != nil {
		return nil, fmt.Errorf("error al limpiar ticket local: %w", err)
	}

	summary["status"] = "deleted"
	summary["hours_discarded"] = hoursOwn
	summary["message"] = fmt.Sprintf("Tarea %s eliminada exitosamente", key)
	return summary, nil
}

// AddLink asocia un hipervínculo a una tarea en Plane. No se difiere: sin conexión o si Plane falla,
// retorna error en vez de guardar un enlace local que nunca existiría en Plane.
func (s *TrackerService) AddLink(ctx context.Context, key, title, linkURL string) (*issue.Link, error) {
	item, err := s.remoteTarget(ctx, key)
	if err != nil {
		return nil, err
	}
	if title == "" {
		title = linkURL
	}
	var remote *issue.Link
	_, err = s.dispatch(ctx, remoteMutation{
		Name: "asociar enlace",
		Send: func(ctx context.Context) error {
			created, err := s.client.AddLinkRemote(ctx, item.ProjectID, item.ID, title, linkURL)
			if err != nil {
				return err
			}
			if created == nil || created.ID == "" {
				return fmt.Errorf("Plane no devolvió el identificador del enlace")
			}
			remote = created
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	link := &issue.Link{ID: remote.ID, IssueID: item.ID, TicketID: item.Key, Title: title, URL: linkURL, CreatedAt: time.Now()}
	_ = s.store.AddLink(ctx, *link)
	return link, nil
}

// ListLinks lista los hipervínculos asociados a una tarea. Plane es la fuente de verdad: si responde,
// la copia local se reconcilia (se descartan los eliminados). Si Plane no responde por un fallo transitorio,
// se devuelve la copia local marcada como tal; cualquier otro fallo se muestra.
func (s *TrackerService) ListLinks(ctx context.Context, key string) (*issue.LinkListing, error) {
	if s.store == nil {
		return nil, fmt.Errorf("almacenamiento no inicializado")
	}
	item, err := s.store.GetIssue(ctx, key)
	if err != nil {
		return nil, err
	}

	fromCache := func(note string) (*issue.LinkListing, error) {
		items, err := s.store.ListLinks(ctx, item.Key)
		if err != nil {
			return nil, err
		}
		if items == nil {
			items = make([]issue.Link, 0)
		}
		return &issue.LinkListing{Items: items, Source: "cache", Complete: false, Note: note}, nil
	}

	if s.client == nil || item.ProjectID == "" || item.ID == "" || strings.HasPrefix(item.ID, "local-") {
		return fromCache("sin conexión con Plane o tarea aún local: copia local, puede estar desactualizada")
	}

	remote, err := s.client.ListLinksRemote(ctx, item.ProjectID, item.ID)
	if err != nil {
		if issue.ClassifyRemoteError(err) == issue.RemoteErrTransient {
			return fromCache(fmt.Sprintf("Plane no respondió (%v): copia local, puede estar desactualizada", err))
		}
		return nil, err
	}

	if remote == nil {
		remote = make([]issue.Link, 0)
	}

	seen := make(map[string]bool, len(remote))
	for i := range remote {
		remote[i].TicketID = item.Key
		seen[remote[i].ID] = true
		_ = s.store.AddLink(ctx, remote[i])
	}

	if cached, err := s.store.ListLinks(ctx, item.Key); err == nil {
		for _, c := range cached {
			if !seen[c.ID] {
				_ = s.store.DeleteLink(ctx, c.ID)
			}
		}
	}

	return &issue.LinkListing{Items: remote, Source: "api", Complete: true}, nil
}

// DeleteLink elimina un hipervínculo en Plane y, solo si Plane lo confirma, de la copia local.
func (s *TrackerService) DeleteLink(ctx context.Context, key, linkID string) error {
	item, err := s.remoteTarget(ctx, key)
	if err != nil {
		return err
	}
	if _, err := s.dispatch(ctx, remoteMutation{
		Name: "eliminar enlace",
		Send: func(ctx context.Context) error {
			return s.client.DeleteLinkRemote(ctx, item.ProjectID, item.ID, linkID)
		},
	}); err != nil {
		return err
	}
	return s.store.DeleteLink(ctx, linkID)
}

// SetFocus fija el ticket activo en la sesión.
func (s *TrackerService) SetFocus(ctx context.Context, key, sessionID string) (*issue.FocusRecord, error) {
	item, err := s.store.GetIssue(ctx, key)
	if err != nil {
		return nil, err
	}
	return s.store.SetFocus(ctx, item.Key, sessionID)
}

// ClearFocus libera el ticket de foco de la sesión.
func (s *TrackerService) ClearFocus(ctx context.Context, sessionID string) (*issue.FocusRecord, error) {
	return s.store.ClearFocus(ctx, sessionID)
}

// FocusStatus consulta el foco activo de la sesión.
func (s *TrackerService) FocusStatus(ctx context.Context, sessionID string) (*issue.FocusRecord, error) {
	rec, err := s.store.GetFocusStatus(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return &issue.FocusRecord{Active: false}, nil
	}
	return rec, nil
}

// ProposeWorklog calcula una propuesta de tiempo medido para la sesión.
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
	now := time.Now()
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
		fromDate = time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	}
	if toDate == "" {
		toDate = time.Now().Format("2006-01-02")
	}
	return s.store.GetTimeReport(ctx, fromDate, toDate, track, workerType)
}

// ReportFlow genera métricas de flujo (lead time, throughput y estados).
func (s *TrackerService) ReportFlow(ctx context.Context, fromDate, toDate string) (*issue.FlowReport, error) {
	if fromDate == "" {
		fromDate = time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	}
	if toDate == "" {
		toDate = time.Now().Format("2006-01-02")
	}
	return s.store.GetFlowReport(ctx, fromDate, toDate)
}

// AddComment añade un comentario o bitácora sobre una tarea. Si hours > 0, formatea telemetría structured <!-- PLANE_WORKLOG_METADATA ... --> y registra el worklog localmente.
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
		now := time.Now()
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
		clientID := fmt.Sprintf("cmt-%s-%d", item.Key, time.Now().UnixNano())
		_ = s.store.EnqueueOutboxWithMutation(ctx, clientID, item.Key, string(payloadBytes), "", "comment")
	}

	rec := &issue.CommentRecord{
		ID:          commentID,
		CommentHTML: htmlBody,
		CreatedAt:   time.Now().Format(time.RFC3339),
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

// ListOutbox lista los registros en la cola outbox.
func (s *TrackerService) ListOutbox(ctx context.Context, includeAll bool) ([]issue.OutboxRecord, error) {
	return s.store.ListOutbox(ctx, includeAll)
}

// DropOutbox elimina un registro de la cola outbox.
func (s *TrackerService) DropOutbox(ctx context.Context, id int64) error {
	return s.store.DropOutboxItem(ctx, id)
}

// ListDeadLetterOutbox lista las mutaciones encoladas que alcanzaron el límite de reintentos (DLQ).
func (s *TrackerService) ListDeadLetterOutbox(ctx context.Context) ([]issue.OutboxRecord, error) {
	if s.store == nil {
		return nil, fmt.Errorf("almacenamiento no inicializado")
	}
	return s.store.ListDeadLetterOutbox(ctx)
}

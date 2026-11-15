package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/adapters/tracker_plane"
	"github.com/allostasis/alo/internal/domain/issue"
)

// TrackerService orquesta la gestión soberana de tareas (local-first con sincronización upstream).
type TrackerService struct {
	store  *sqlite.Store
	client *tracker_plane.Client
}

// NewTrackerService construye el servicio orquestador de tareas.
func NewTrackerService(store *sqlite.Store, client *tracker_plane.Client) *TrackerService {
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
	if err == nil {
		projectID = pRec.ProjectID
	}

	// 2. Resolver stateID si se indicó StateName
	var stateID, stateGroup string
	if input.StateName != "" && projectID != "" {
		if stRec, err := s.store.GetState(ctx, projectID, input.StateName); err == nil {
			stateID = stRec.ID
			stateGroup = stRec.GroupName
		}
	}
	if stateGroup == "" {
		stateGroup = "backlog"
	}

	// 3. Intentar creación remota en Plane CE si el cliente está disponible
	if s.client != nil && projectID != "" {
		created, err := s.client.CreateIssueRemote(ctx, projectID, input, stateID)
		if err == nil {
			_ = s.store.SaveIssue(ctx, created)
			rawJSON, _ := json.Marshal(created)
			_ = s.store.SaveIssueSnapshot(ctx, created.ID, created.Key, string(rawJSON))
			return created, nil
		}
	}

	// 4. Si Plane está offline o falla, crear en local-first con cola outbox
	nowUnix := time.Now().Unix()
	localUUID := fmt.Sprintf("local-%d", nowUnix)
	localKey := fmt.Sprintf("%s-LOCAL-%d", input.ProjectKey, nowUnix%10000)

	localItem := &issue.WorkItem{
		ID:          localUUID,
		Key:         localKey,
		ProjectID:   projectID,
		ProjectKey:  input.ProjectKey,
		Title:       input.Title,
		Description: input.Description,
		StateName:   input.StateName,
		StateGroup:  stateGroup,
		State:       issue.State(stateGroup),
		Priority:    input.Priority,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := s.store.SaveIssue(ctx, localItem); err != nil {
		return nil, fmt.Errorf("error al guardar tarea en caché local: %w", err)
	}

	// Encolar evento outbox para reconciliación diferida
	payloadBytes, _ := json.Marshal(input)
	outboxEvent := &issue.OutboxEvent{
		ID:        fmt.Sprintf("outbox-%d", nowUnix),
		WorkItem:  localKey,
		Payload:   string(payloadBytes),
		Status:    issue.OutboxPending,
		CreatedAt: time.Now(),
	}
	_ = s.store.EnqueueOutbox(ctx, outboxEvent)

	return localItem, nil
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
			if stRec, err := s.store.GetState(ctx, item.ProjectID, *input.StateName); err == nil {
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

	// Guardar cambio local en SQLite
	if err := s.store.SaveIssue(ctx, item); err != nil {
		return nil, fmt.Errorf("error actualizando tarea en SQLite: %w", err)
	}

	// Intentar mutación upstream en Plane CE si hay conexión
	if s.client != nil && item.ProjectID != "" && item.ID != "" && !strings.HasPrefix(item.ID, "local-") {
		_ = s.client.UpdateIssueRemote(ctx, item.ProjectID, item.ID, input, stateID)
	}

	return item, nil
}

// Sync sincroniza proyectos, estados y tareas desde Plane CE hacia la base de datos local.
func (s *TrackerService) Sync(ctx context.Context, projectKey string, allProjects bool) (int, error) {
	if s.client == nil {
		return 0, fmt.Errorf("cliente de Plane no configurado")
	}

	// 1. Sincronizar catálogo de proyectos
	projects, err := s.client.SyncProjects(ctx, s.store)
	if err != nil {
		return 0, fmt.Errorf("error sincronizando proyectos: %w", err)
	}

	totalSynced := 0
	if allProjects {
		for _, p := range projects {
			count, err := s.client.SyncIssues(ctx, s.store, p.Identifier, 100)
			if err == nil {
				totalSynced += count
			}
		}
	} else {
		if projectKey == "" {
			projectKey = "OMNIFY"
		}
		count, err := s.client.SyncIssues(ctx, s.store, projectKey, 100)
		if err != nil {
			return 0, err
		}
		totalSynced = count
	}

	return totalSynced, nil
}

// Whoami consulta la identidad y validez de la sesión en Plane CE.
func (s *TrackerService) Whoami(ctx context.Context) (*tracker_plane.UserInfo, error) {
	if s.client == nil {
		return nil, fmt.Errorf("cliente de Plane no configurado")
	}
	return s.client.Whoami(ctx)
}

// Projects retorna los proyectos registrados en la caché local.
func (s *TrackerService) Projects(ctx context.Context) ([]sqlite.ProjectRecord, error) {
	return s.store.ListProjects(ctx)
}

package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/app"
	"github.com/allostasis/alo/internal/domain/issue"
)

// forbiddenProjectsClient simula un workspace con varios proyectos donde el usuario
// no tiene permiso sobre algunos: Plane responde 403 al listar sus tareas.
type forbiddenProjectsClient struct {
	*mockTrackerClient
	projects  []issue.ProjectRecord
	forbidden map[string]int // projectID -> status HTTP con el que se rechaza
}

func (c *forbiddenProjectsClient) FetchProjects(ctx context.Context) ([]issue.ProjectRecord, error) {
	return c.projects, nil
}

func (c *forbiddenProjectsClient) FetchIssuesDelta(ctx context.Context, projectID, projectKey string, limit int, cutoffISO string, deep bool) ([]issue.IssueSnapshot, string, error) {
	if status, ok := c.forbidden[projectID]; ok {
		return nil, "", &issue.AuthError{Status: status, Operation: "listar tareas", Detail: "sin permiso"}
	}
	return c.mockTrackerClient.FetchIssuesDelta(ctx, projectID, projectKey, limit, cutoffISO, deep)
}

func newForbiddenSvc(t *testing.T, forbidden map[string]int, projects ...issue.ProjectRecord) *app.TrackerService {
	t.Helper()
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error abriendo store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	client := &forbiddenProjectsClient{
		mockTrackerClient: &mockTrackerClient{sentComments: make(map[string]string)},
		projects:          projects,
		forbidden:         forbidden,
	}
	return app.NewTrackerService(store, client)
}

var (
	projOmnify = issue.ProjectRecord{Identifier: "OMNIFY", ProjectID: "proj-1", Name: "Omnify"}
	projLeads  = issue.ProjectRecord{Identifier: "OMNILEADS", ProjectID: "proj-2", Name: "Omnileads"}
	projOps    = issue.ProjectRecord{Identifier: "OPS", ProjectID: "proj-3", Name: "Ops"}
)

// Un 403 sobre un proyecto concreto no es un error del sync: se omite y se informa aparte.
func TestSyncUnified_ForbiddenProjectIsSkippedNotAnError(t *testing.T) {
	svc := newForbiddenSvc(t, map[string]int{"proj-2": 403}, projOmnify, projLeads, projOps)

	rep, err := svc.SyncUnified(context.Background(), issue.SyncUnifiedOptions{Target: "tickets"})
	if err != nil {
		t.Fatalf("SyncUnified falló: %v", err)
	}
	if len(rep.Errors) != 0 {
		t.Fatalf("un 403 por proyecto no debe ser error; errors=%v", rep.Errors)
	}
	if rep.Status != "synced" {
		t.Errorf("estado inesperado %q; el 403 omitido no debe degradarlo", rep.Status)
	}
	if len(rep.SkippedProjects) != 1 || rep.SkippedProjects[0].Project != "OMNILEADS" {
		t.Fatalf("skipped_projects inesperado: %+v", rep.SkippedProjects)
	}
	if _, ok := rep.Watermarks["OMNIFY"]; !ok {
		t.Errorf("los proyectos con acceso deben sincronizarse; watermarks=%v", rep.Watermarks)
	}
	if _, ok := rep.Watermarks["OMNILEADS"]; ok {
		t.Errorf("el proyecto omitido no debe registrar watermark")
	}
	if rep.SyncedIssues == 0 {
		t.Errorf("las tareas de los proyectos con acceso deben descargarse")
	}
}

// Pedir el proyecto explícitamente (-p) y que dé 403 SÍ debe reportarse como error.
func TestSyncUnified_ExplicitForbiddenProjectIsAnError(t *testing.T) {
	svc := newForbiddenSvc(t, map[string]int{"proj-2": 403}, projOmnify, projLeads)

	rep, err := svc.SyncUnified(context.Background(), issue.SyncUnifiedOptions{Target: "tickets", ProjectKey: "OMNILEADS"})
	if err != nil {
		t.Fatalf("SyncUnified falló: %v", err)
	}
	if len(rep.SkippedProjects) != 0 {
		t.Errorf("un proyecto pedido explícitamente no debe omitirse en silencio: %+v", rep.SkippedProjects)
	}
	if len(rep.Errors) != 1 || !strings.Contains(rep.Errors[0], "OMNILEADS") {
		t.Fatalf("debe reportar el 403 como error; errors=%v", rep.Errors)
	}
}

// Si TODOS los proyectos dan 403 el problema son las credenciales/permisos: debe seguir siendo error.
func TestSyncUnified_AllProjectsForbiddenStaysAnError(t *testing.T) {
	svc := newForbiddenSvc(t, map[string]int{"proj-1": 403, "proj-2": 403}, projOmnify, projLeads)

	rep, err := svc.SyncUnified(context.Background(), issue.SyncUnifiedOptions{Target: "tickets"})
	if err != nil {
		t.Fatalf("SyncUnified falló: %v", err)
	}
	if len(rep.Errors) != 2 {
		t.Fatalf("todos 403 debe reportar un error por proyecto; errors=%v", rep.Errors)
	}
	if len(rep.SkippedProjects) != 0 {
		t.Errorf("no debe quedar nada 'omitido' si falló todo: %+v", rep.SkippedProjects)
	}
	if rep.Status != "failed" {
		t.Errorf("estado esperado failed, obtuvo %q", rep.Status)
	}
}

// El 401 (credenciales inválidas) nunca se omite, aunque sea un solo proyecto.
func TestSyncUnified_UnauthorizedIsNeverSkipped(t *testing.T) {
	svc := newForbiddenSvc(t, map[string]int{"proj-2": 401}, projOmnify, projLeads)

	rep, err := svc.SyncUnified(context.Background(), issue.SyncUnifiedOptions{Target: "tickets"})
	if err != nil {
		t.Fatalf("SyncUnified falló: %v", err)
	}
	if len(rep.SkippedProjects) != 0 {
		t.Errorf("el 401 no debe omitirse: %+v", rep.SkippedProjects)
	}
	if len(rep.Errors) != 1 {
		t.Fatalf("el 401 debe reportarse como error; errors=%v", rep.Errors)
	}
}

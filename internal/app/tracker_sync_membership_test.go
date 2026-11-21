package app_test

import (
	"context"
	"sort"
	"sync"
	"testing"

	"github.com/JohanTv/allostasis/internal/adapters/sqlite"
	"github.com/JohanTv/allostasis/internal/app"
	"github.com/JohanTv/allostasis/internal/domain/issue"
)

// recordingClient registra a qué proyectos se les pidió estados, etiquetas y tareas (el ahorro de peticiones es el objetivo).
type recordingClient struct {
	*mockTrackerClient
	projects []issue.ProjectRecord
	mu       sync.Mutex
	states   map[string]int
	labels   map[string]int
	issues   map[string]int
}

func (c *recordingClient) FetchProjects(ctx context.Context) ([]issue.ProjectRecord, error) {
	return c.projects, nil
}
func (c *recordingClient) FetchStates(ctx context.Context, projectID string) ([]issue.StateRecord, error) {
	c.mu.Lock()
	c.states[projectID]++
	c.mu.Unlock()
	return c.mockTrackerClient.FetchStates(ctx, projectID)
}
func (c *recordingClient) FetchLabels(ctx context.Context, projectID string) ([]issue.Label, error) {
	c.mu.Lock()
	c.labels[projectID]++
	c.mu.Unlock()
	return c.mockTrackerClient.FetchLabels(ctx, projectID)
}
func (c *recordingClient) FetchIssuesDelta(ctx context.Context, projectID, projectKey string, limit int, cutoffISO string, deep bool) ([]issue.IssueSnapshot, string, error) {
	c.mu.Lock()
	c.issues[projectID]++
	c.mu.Unlock()
	return c.mockTrackerClient.FetchIssuesDelta(ctx, projectID, projectKey, limit, cutoffISO, deep)
}

func bp(b bool) *bool { return &b }

func newRecordingSvc(t *testing.T, projects ...issue.ProjectRecord) (*app.TrackerService, *recordingClient) {
	t.Helper()
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error abriendo store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	rc := &recordingClient{
		mockTrackerClient: &mockTrackerClient{sentComments: make(map[string]string)},
		projects:          projects,
		states:            map[string]int{}, labels: map[string]int{}, issues: map[string]int{},
	}
	return app.NewTrackerService(store, rc), rc
}

func keys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var (
	pMember  = issue.ProjectRecord{Identifier: "OMNIFY", ProjectID: "proj-1", Name: "Omnify", IsMember: bp(true)}
	pNotMem  = issue.ProjectRecord{Identifier: "OMNILEADS", ProjectID: "proj-2", Name: "Omnileads", IsMember: bp(false)}
	pUnknown = issue.ProjectRecord{Identifier: "OPS", ProjectID: "proj-3", Name: "Ops"} // pertenencia desconocida
)

// El sync por defecto no hace NINGUNA petición (estados, etiquetas ni tareas) a un proyecto donde no es miembro.
func TestSyncUnified_DefaultSkipsNonMemberProjectsWithoutRequests(t *testing.T) {
	svc, rc := newRecordingSvc(t, pMember, pNotMem, pUnknown)

	rep, err := svc.SyncUnified(context.Background(), issue.SyncUnifiedOptions{Target: "all"})
	if err != nil {
		t.Fatalf("SyncUnified: %v", err)
	}
	want := []string{"proj-1", "proj-3"} // miembro + desconocido
	if got := keys(rc.issues); !eq(got, want) {
		t.Errorf("tareas pedidas a %v, se esperaba %v", got, want)
	}
	if got := keys(rc.states); !eq(got, want) {
		t.Errorf("estados pedidos a %v, se esperaba %v", got, want)
	}
	if got := keys(rc.labels); !eq(got, want) {
		t.Errorf("etiquetas pedidas a %v, se esperaba %v", got, want)
	}
	if len(rep.Errors) != 0 || len(rep.SkippedProjects) != 0 {
		t.Errorf("sin peticiones al no miembro no debe haber errores ni 'omitidos por 403': %+v %+v", rep.Errors, rep.SkippedProjects)
	}
	if rep.SyncedProjects != 3 {
		t.Errorf("el catálogo sigue listando los 3 proyectos visibles; synced_projects=%d", rep.SyncedProjects)
	}
}

// Lo explícito manda: -p sobre un no miembro se intenta (y el usuario verá el resultado real); --all-projects incluye todo.
func TestSyncUnified_ExplicitAndAllProjectsStillIncludeNonMembers(t *testing.T) {
	svc, rc := newRecordingSvc(t, pMember, pNotMem)
	if _, err := svc.SyncUnified(context.Background(), issue.SyncUnifiedOptions{Target: "tickets", ProjectKey: "OMNILEADS"}); err != nil {
		t.Fatalf("SyncUnified -p: %v", err)
	}
	if rc.issues["proj-2"] != 1 {
		t.Errorf("-p OMNILEADS debe intentarse aunque no sea miembro: %v", rc.issues)
	}

	svc2, rc2 := newRecordingSvc(t, pMember, pNotMem)
	if _, err := svc2.SyncUnified(context.Background(), issue.SyncUnifiedOptions{Target: "tickets", AllProjects: true}); err != nil {
		t.Fatalf("SyncUnified --all-projects: %v", err)
	}
	if got := keys(rc2.issues); !eq(got, []string{"proj-1", "proj-2"}) {
		t.Errorf("--all-projects debe incluir todos: %v", got)
	}
}

// SyncCatalogs (sin proyecto) también omite a los no miembros.
func TestSyncCatalogs_SkipsNonMemberProjects(t *testing.T) {
	svc, rc := newRecordingSvc(t, pMember, pNotMem)
	if err := svc.SyncCatalogs(context.Background(), ""); err != nil {
		t.Fatalf("SyncCatalogs: %v", err)
	}
	if got := keys(rc.states); !eq(got, []string{"proj-1"}) {
		t.Errorf("estados pedidos a %v, se esperaba solo proj-1", got)
	}
}

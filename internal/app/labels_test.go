package app_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/app"
	"github.com/allostasis/alo/internal/domain/issue"
)

const (
	lblFrontend = "11111111-1111-1111-1111-111111111111"
	lblCorreo   = "22222222-2222-2222-2222-222222222222"
)

// labelsClient simula a Plane: sirve el catálogo de etiquetas y captura lo que recibe (siempre UUID).
type labelsClient struct {
	*mockTrackerClient
	catalog    []issue.Label
	fetchErr   error
	fetchCalls int
	createErr  error
	updateErr  error
	created    []issue.CreateIssueInput
	updated    []issue.UpdateIssueInput
}

func (c *labelsClient) FetchLabels(context.Context, string) ([]issue.Label, error) {
	c.fetchCalls++
	return c.catalog, c.fetchErr
}

func (c *labelsClient) CreateIssueRemote(_ context.Context, projectID string, in issue.CreateIssueInput, _ string) (*issue.WorkItem, error) {
	if c.createErr != nil {
		return nil, c.createErr
	}
	c.created = append(c.created, in)
	// Como el cliente real: devuelve lo que se le envió (UUID), no los nombres.
	return &issue.WorkItem{ID: "iss-new", Key: "OMNIFY-100", ProjectID: projectID, Title: in.Title, Labels: in.Labels}, nil
}

func (c *labelsClient) UpdateIssueRemote(_ context.Context, _, _ string, in issue.UpdateIssueInput, _ string) error {
	c.updated = append(c.updated, in)
	return c.updateErr
}

func newLabelsEnv(t *testing.T, cached []issue.Label, client *labelsClient) (*app.TrackerService, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error abriendo store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	if err := store.SaveProject(ctx, issue.ProjectRecord{Identifier: "OMNIFY", ProjectID: "proj-1", Name: "Omnify"}); err != nil {
		t.Fatalf("error guardando proyecto: %v", err)
	}
	for _, l := range cached {
		if err := store.SaveLabel(ctx, l); err != nil {
			t.Fatalf("error guardando etiqueta: %v", err)
		}
	}
	client.mockTrackerClient = &mockTrackerClient{}
	return app.NewTrackerService(store, client), store
}

var fullCatalog = []issue.Label{
	{ID: lblFrontend, ProjectID: "proj-1", Name: "frontend"},
	{ID: lblCorreo, ProjectID: "proj-1", Name: "Correo"},
}

func newCreate(labels ...string) issue.CreateIssueInput {
	return issue.CreateIssueInput{ProjectKey: "OMNIFY", Title: "t", Priority: "medium", Labels: labels}
}

func TestCreate_LabelsResolvedFromLocalCatalog_NoNetwork(t *testing.T) {
	client := &labelsClient{}
	svc, store := newLabelsEnv(t, fullCatalog, client)
	ctx := context.Background()

	// Nombre exacto, otra capitalización, UUID directo y duplicado.
	if _, err := svc.Create(ctx, newCreate("frontend", "correo", lblCorreo, "FRONTEND")); err != nil {
		t.Fatalf("Create falló: %v", err)
	}
	if got, want := client.created[0].Labels, []string{lblFrontend, lblCorreo}; !reflect.DeepEqual(got, want) {
		t.Errorf("Plane recibió %v, esperado UUIDs %v", got, want)
	}
	if client.fetchCalls != 0 {
		t.Errorf("con el catálogo local completo no debe consultarse Plane (%d llamadas)", client.fetchCalls)
	}
	// La caché local guarda los nombres que escribió el usuario, no los UUID.
	saved, _ := store.GetIssue(ctx, "OMNIFY-100")
	if saved.LabelsCSV != "frontend,correo,"+lblCorreo+",FRONTEND" {
		t.Errorf("la caché debe conservar los nombres escritos, tiene %q", saved.LabelsCSV)
	}
}

func TestCreate_MissingLabelRefreshesCatalogOnce(t *testing.T) {
	client := &labelsClient{catalog: fullCatalog}
	svc, store := newLabelsEnv(t, nil, client) // catálogo local vacío
	ctx := context.Background()

	if _, err := svc.Create(ctx, newCreate("frontend")); err != nil {
		t.Fatalf("Create falló: %v", err)
	}
	if client.fetchCalls != 1 {
		t.Errorf("se esperaba 1 refresco del catálogo, hubo %d", client.fetchCalls)
	}
	if cached, _ := store.ListLabels(ctx, "proj-1"); len(cached) != 2 {
		t.Errorf("el refresco debe poblar el catálogo local, tiene %d", len(cached))
	}
}

func TestCreate_UnknownLabelAfterRefresh_FailsClosed(t *testing.T) {
	client := &labelsClient{catalog: fullCatalog}
	svc, store := newLabelsEnv(t, fullCatalog, client)
	ctx := context.Background()

	_, err := svc.Create(ctx, newCreate("frontend", "tts-typo"))
	var unknown *issue.UnknownLabelError
	if !errors.As(err, &unknown) {
		t.Fatalf("esperado *issue.UnknownLabelError, obtuvo %v", err)
	}
	// El catálogo local lista las válidas ordenadas por nombre.
	if !reflect.DeepEqual(unknown.Names, []string{"tts-typo"}) || !reflect.DeepEqual(unknown.Valid, []string{"Correo", "frontend"}) {
		t.Errorf("Names=%v Valid=%v", unknown.Names, unknown.Valid)
	}
	if len(client.created) != 0 {
		t.Error("no debe llamarse a Plane con etiquetas inválidas")
	}
	if pending, _ := store.ListOutbox(ctx, true); len(pending) != 0 {
		t.Errorf("un error de entrada no se encola (hay %d en outbox)", len(pending))
	}
}

func TestCreate_OfflineWithUnknownLabel_QueuesWithNames_ThenReplaysWithIDs(t *testing.T) {
	client := &labelsClient{fetchErr: errors.New("sin red")}
	svc, store := newLabelsEnv(t, nil, client)
	ctx := context.Background()

	local, err := svc.Create(ctx, newCreate("frontend"))
	if err != nil {
		t.Fatalf("offline no debe fallar: %v", err)
	}
	if len(client.created) != 0 {
		t.Error("sin catálogo no se debe enviar a Plane")
	}
	if pending, _ := store.ListOutbox(ctx, false); len(pending) != 1 {
		t.Fatalf("se esperaba 1 mutación en outbox, hay %d", len(pending))
	}

	// Vuelve la conexión: el replay resuelve los nombres a UUID.
	client.fetchErr, client.catalog = nil, fullCatalog
	if _, err := svc.FlushOutbox(ctx, 10); err != nil {
		t.Fatalf("FlushOutbox falló: %v", err)
	}
	if len(client.created) != 1 || !reflect.DeepEqual(client.created[0].Labels, []string{lblFrontend}) {
		t.Errorf("el replay debe enviar UUIDs, envió %+v", client.created)
	}
	_ = local
}

func TestUpdate_Labels_ResolvedClearedAndRejected(t *testing.T) {
	client := &labelsClient{}
	svc, store := newLabelsEnv(t, fullCatalog, client)
	ctx := context.Background()
	if err := store.SaveIssue(ctx, &issue.WorkItem{ID: "iss-2", Key: "OMNIFY-2", ProjectID: "proj-1", Title: "t", Labels: []string{"frontend"}, LabelsCSV: "frontend"}); err != nil {
		t.Fatalf("error guardando issue: %v", err)
	}

	names := []string{"Correo"}
	if _, err := svc.Update(ctx, "OMNIFY-2", issue.UpdateIssueInput{Labels: &names}); err != nil {
		t.Fatalf("Update falló: %v", err)
	}
	if got := *client.updated[0].Labels; !reflect.DeepEqual(got, []string{lblCorreo}) {
		t.Errorf("Plane recibió %v, esperado [%s]", got, lblCorreo)
	}

	none := []string{}
	if _, err := svc.Update(ctx, "OMNIFY-2", issue.UpdateIssueInput{Labels: &none}); err != nil {
		t.Fatalf("Update (vaciar) falló: %v", err)
	}
	if got := client.updated[1].Labels; got == nil || len(*got) != 0 {
		t.Errorf("vaciar etiquetas debe enviar lista vacía, envió %v", got)
	}

	// Etiqueta inexistente: error, Plane no se llama y la caché conserva lo anterior.
	bad := []string{"typo"}
	before := len(client.updated)
	var unknown *issue.UnknownLabelError
	if _, err := svc.Update(ctx, "OMNIFY-2", issue.UpdateIssueInput{Labels: &bad}); !errors.As(err, &unknown) {
		t.Fatalf("esperado *issue.UnknownLabelError, obtuvo %v", err)
	}
	if len(client.updated) != before {
		t.Error("no debe llamarse a Plane con etiquetas inválidas")
	}
	if got, _ := store.GetIssue(ctx, "OMNIFY-2"); got.LabelsCSV != "" {
		t.Errorf("la caché debe quedar como tras la última actualización válida (vacía), tiene %q", got.LabelsCSV)
	}
}

// La política única (ADR 0032): solo lo transitorio se encola; todo lo demás, incluido lo que nadie clasificó, se muestra.
func TestCreate_DispatchPolicy(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantErr    bool
		wantOutbox int
	}{
		{"rechazo 400: se muestra", &issue.RejectedError{Status: 400, Operation: "crear tarea", Detail: "bad"}, true, 0},
		{"transitorio 500: se encola", &issue.TransientError{Status: 500, Operation: "crear tarea"}, false, 1},
		{"red caída: se encola", &issue.TransientError{Operation: "crear tarea", Cause: errors.New("connection refused")}, false, 1},
		{"credenciales 401: se muestra", &issue.AuthError{Status: 401, Operation: "crear tarea"}, true, 0},
		{"error sin clasificar: se muestra, no se pierde", errors.New("algo que nadie mapeó"), true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &labelsClient{createErr: tc.err}
			svc, store := newLabelsEnv(t, nil, client)
			item, err := svc.Create(context.Background(), newCreate())
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if pending, _ := store.ListOutbox(context.Background(), true); len(pending) != tc.wantOutbox {
				t.Errorf("outbox = %d, esperado %d", len(pending), tc.wantOutbox)
			}
			if tc.wantOutbox == 1 && (item == nil || !item.PendingSync) {
				t.Errorf("una mutación encolada debe marcarse pending_sync para que se informe al usuario: %+v", item)
			}
		})
	}
}

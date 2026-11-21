package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/app"
	"github.com/allostasis/alo/internal/domain/issue"
)

// scriptedClient es un Plane programable para las pruebas de despacho: estados, fallos por operación
// y registro de lo que recibió, con el orden de las llamadas.
type scriptedClient struct {
	*mockTrackerClient
	states []issue.StateRecord

	updateErr, uploadErr, listErr, deleteErr, linkErr error
	uploadFailName                                    string // si no está vacío, solo falla la subida de ese archivo
	listItems                                         []issue.Attachment
	listComplete                                      bool
	listLinksItems                                    []issue.Link
	listLinksErr                                      error

	calls      []string
	updates    []issue.UpdateIssueInput
	updateSIDs []string
	uploads    []string // nombre:contenido
	deleted    []string
}

func (c *scriptedClient) FetchStates(context.Context, string) ([]issue.StateRecord, error) {
	return c.states, nil
}

func (c *scriptedClient) UpdateIssueRemote(_ context.Context, _, _ string, in issue.UpdateIssueInput, stateID string) error {
	c.calls = append(c.calls, "update")
	if c.updateErr != nil {
		return c.updateErr
	}
	c.updates = append(c.updates, in)
	c.updateSIDs = append(c.updateSIDs, stateID)
	return nil
}

func (c *scriptedClient) UploadAttachmentRemote(_ context.Context, _, issueID string, up issue.AttachmentUpload) (*issue.Attachment, error) {
	c.calls = append(c.calls, "upload:"+up.Name)
	if c.uploadErr != nil && (c.uploadFailName == "" || c.uploadFailName == up.Name) {
		return nil, c.uploadErr
	}
	data := make([]byte, up.Size)
	n, _ := up.Content.Read(data)
	c.uploads = append(c.uploads, up.Name+":"+string(data[:n]))
	return &issue.Attachment{ID: fmt.Sprintf("bbbbbbbb-0000-0000-0000-%012d", len(c.uploads)), IssueID: issueID, Name: up.Name, SizeBytes: up.Size}, nil
}

func (c *scriptedClient) ListAttachmentsRemote(context.Context, string, string) ([]issue.Attachment, bool, error) {
	c.calls = append(c.calls, "list")
	return c.listItems, c.listComplete, c.listErr
}

func (c *scriptedClient) DeleteAttachmentRemote(_ context.Context, _, _, id string) error {
	c.calls = append(c.calls, "delete:"+id)
	if c.deleteErr != nil {
		return c.deleteErr
	}
	c.deleted = append(c.deleted, id)
	return nil
}

func (c *scriptedClient) AddLinkRemote(_ context.Context, _, issueID, title, url string) (*issue.Link, error) {
	if c.linkErr != nil {
		return nil, c.linkErr
	}
	return &issue.Link{ID: "link-remote-1", IssueID: issueID, Title: title, URL: url}, nil
}

func (c *scriptedClient) ListLinksRemote(context.Context, string, string) ([]issue.Link, error) {
	c.calls = append(c.calls, "list_links")
	if c.listLinksErr != nil {
		return nil, c.listLinksErr
	}
	return c.listLinksItems, nil
}

func (c *scriptedClient) DeleteLinkRemote(context.Context, string, string, string) error {
	return c.linkErr
}

// newScriptedEnv arma un store con un proyecto, estados y una tarea remota (OMNIFY-2) y un servicio sobre `client`
// (nil = modo sin conexión explícito).
func newScriptedEnv(t *testing.T, client *scriptedClient) (*app.TrackerService, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error abriendo store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	_ = store.SaveProject(ctx, issue.ProjectRecord{Identifier: "OMNIFY", ProjectID: "proj-1", Name: "Omnify"})
	for _, st := range []issue.StateRecord{
		{ID: "st-prog", ProjectID: "proj-1", Name: "Progreso", Slug: "progreso", GroupName: "started"},
		{ID: "st-done", ProjectID: "proj-1", Name: "Completado", Slug: "completado", GroupName: "completed"},
	} {
		_ = store.SaveState(ctx, st)
	}
	for _, l := range fullCatalog {
		_ = store.SaveLabel(ctx, l)
	}
	if err := store.SaveIssue(ctx, &issue.WorkItem{ID: "iss-2", Key: "OMNIFY-2", ProjectID: "proj-1", ProjectKey: "OMNIFY", Title: "t", Priority: "low"}); err != nil {
		t.Fatalf("error guardando issue: %v", err)
	}
	var svc *app.TrackerService
	if client == nil {
		svc = app.NewTrackerService(store, nil)
	} else {
		client.mockTrackerClient = &mockTrackerClient{}
		svc = app.NewTrackerService(store, client)
	}
	return svc, store
}

func prio(p string) issue.UpdateIssueInput { return issue.UpdateIssueInput{Priority: &p} }

func TestUpdate_DispatchPolicy(t *testing.T) {
	cases := []struct {
		name        string
		err         error
		wantErr     bool
		wantPending bool
		wantOutbox  int
	}{
		{"éxito: aplicado, nada en outbox", nil, false, false, 0},
		{"transitorio: se encola y se informa", &issue.TransientError{Status: 503, Operation: "actualizar tarea"}, false, true, 1},
		{"rechazo: se muestra", &issue.RejectedError{Status: 422, Operation: "actualizar tarea"}, true, false, 0},
		{"credenciales: se muestra", &issue.AuthError{Status: 403, Operation: "actualizar tarea"}, true, false, 0},
		{"sin clasificar: se muestra", errors.New("no mapeado"), true, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &scriptedClient{updateErr: tc.err}
			svc, store := newScriptedEnv(t, client)
			ctx := context.Background()

			item, err := svc.Update(ctx, "OMNIFY-2", prio("high"))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if err == nil && item.PendingSync != tc.wantPending {
				t.Errorf("PendingSync = %v, esperado %v", item.PendingSync, tc.wantPending)
			}
			outbox, _ := store.ListOutbox(ctx, true)
			if len(outbox) != tc.wantOutbox {
				t.Fatalf("outbox = %d, esperado %d", len(outbox), tc.wantOutbox)
			}
			if tc.wantOutbox == 1 && (outbox[0].MutationType != "update_issue" || outbox[0].TicketRef != "OMNIFY-2") {
				t.Errorf("registro de outbox inesperado: %+v", outbox[0])
			}
			// La caché local solo cambia si Plane lo aceptó o quedó encolado: nunca diverge sin aviso.
			cached, _ := store.GetIssue(ctx, "OMNIFY-2")
			wantPrio := "low"
			if !tc.wantErr {
				wantPrio = "high"
			}
			if cached.Priority != wantPrio {
				t.Errorf("prioridad en caché = %q, esperada %q", cached.Priority, wantPrio)
			}
		})
	}
}

func TestUpdate_OfflineExplicit_QueuesInsteadOfLocalOnly(t *testing.T) {
	svc, store := newScriptedEnv(t, nil) // sin credenciales
	item, err := svc.Update(context.Background(), "OMNIFY-2", prio("urgent"))
	if err != nil || !item.PendingSync {
		t.Fatalf("sin conexión el cambio debe encolarse y marcarse pending: %+v, %v", item, err)
	}
	if outbox, _ := store.ListOutbox(context.Background(), true); len(outbox) != 1 {
		t.Errorf("el cambio no puede quedarse solo en local: outbox = %d", len(outbox))
	}
}

func TestUpdate_OnLocalTicket_QueuesAfterCreate(t *testing.T) {
	client := &scriptedClient{}
	svc, store := newScriptedEnv(t, client)
	ctx := context.Background()
	local, err := svc.Create(ctx, newCreate())
	if err != nil {
		t.Fatal(err)
	}
	// Plane responde pero la creación cayó al outbox (transitorio)? Aquí simulamos tarea local directa:
	_ = store.SaveIssue(ctx, &issue.WorkItem{ID: "local-9", Key: "OMNIFY-LOCAL-9", ProjectID: "proj-1", Title: "pendiente"})
	_ = local

	item, err := svc.Update(ctx, "OMNIFY-LOCAL-9", prio("high"))
	if err != nil || !item.PendingSync {
		t.Fatalf("un cambio sobre una tarea aún local se encola: %+v, %v", item, err)
	}
	if len(client.updates) != 0 {
		t.Error("no debe llamarse a Plane para una tarea que aún no existe allí")
	}
}

func TestUpdate_UnknownState_FailsClosedWithValidOnes(t *testing.T) {
	client := &scriptedClient{states: []issue.StateRecord{{ID: "st-prog", Name: "Progreso"}}}
	svc, store := newScriptedEnv(t, client)
	bad := "Terminado"
	_, err := svc.Update(context.Background(), "OMNIFY-2", issue.UpdateIssueInput{StateName: &bad})
	var unknown *issue.UnknownStateError
	if !errors.As(err, &unknown) || len(unknown.Valid) == 0 {
		t.Fatalf("esperado *UnknownStateError con los estados válidos, obtuvo %v", err)
	}
	if len(client.updates) != 0 {
		t.Error("no debe enviarse a Plane un cambio de estado que se omitiría en silencio")
	}
	if cached, _ := store.GetIssue(context.Background(), "OMNIFY-2"); cached.StateName == "Terminado" {
		t.Error("la caché no debe guardar un estado que no existe")
	}
}

// Cierra el ciclo del defecto: un cambio hecho sin conexión llega a Plane al sincronizar, con el estado y las
// etiquetas ya resueltos a ID.
func TestFlushOutbox_ReplaysQueuedUpdate(t *testing.T) {
	client := &scriptedClient{updateErr: &issue.TransientError{Status: 503, Operation: "actualizar tarea"}}
	svc, store := newScriptedEnv(t, client)
	ctx := context.Background()

	state, labels := "Completado", []string{"correo"}
	item, err := svc.Update(ctx, "OMNIFY-2", issue.UpdateIssueInput{StateName: &state, Labels: &labels})
	if err != nil || !item.PendingSync {
		t.Fatalf("debió encolarse: %+v, %v", item, err)
	}

	// Plane sigue caído: el flush falla, queda registrado y el cambio permanece pendiente.
	report, _ := svc.FlushOutbox(ctx, 10)
	if report.FailedComments != 1 || report.SyncedIssues != 0 {
		t.Fatalf("con Plane caído el replay debe fallar visiblemente: %+v", report)
	}
	pending, _ := store.ListOutbox(ctx, false)
	if len(pending) != 1 || pending[0].LastError == "" {
		t.Fatalf("el cambio debe seguir pendiente con su último error: %+v", pending)
	}

	// Plane vuelve: se envía con IDs.
	client.updateErr = nil
	report, _ = svc.FlushOutbox(ctx, 10)
	if report.SyncedIssues != 1 || len(client.updates) != 1 {
		t.Fatalf("el replay debe aplicar el cambio: %+v, updates=%d", report, len(client.updates))
	}
	if client.updateSIDs[0] != "st-done" {
		t.Errorf("estado enviado = %q, esperado el ID st-done", client.updateSIDs[0])
	}
	if got := *client.updates[0].Labels; !reflect.DeepEqual(got, []string{lblCorreo}) {
		t.Errorf("etiquetas enviadas = %v, esperado el UUID", got)
	}
	if left, _ := store.ListOutbox(ctx, false); len(left) != 0 {
		t.Errorf("tras aplicarse no debe quedar pendiente: %+v", left)
	}
}

func TestFlushOutbox_QueuedUpdateOnPendingCreate_WaitsForIt(t *testing.T) {
	client := &scriptedClient{}
	svc, store := newScriptedEnv(t, client)
	ctx := context.Background()
	_ = store.SaveIssue(ctx, &issue.WorkItem{ID: "local-9", Key: "OMNIFY-LOCAL-9", ProjectID: "proj-1", Title: "pendiente"})
	payload, _ := json.Marshal(prio("high"))
	_ = store.EnqueueOutboxWithMutation(ctx, "c1", "OMNIFY-LOCAL-9", string(payload), "", "update_issue")

	report, _ := svc.FlushOutbox(ctx, 10)
	if report.FailedComments != 1 || len(client.updates) != 0 {
		t.Errorf("un cambio sobre una tarea que aún no existe en Plane debe esperar, no enviarse: %+v", report)
	}
}

func TestLinks_FailLoud_NoFakeLocalRecords(t *testing.T) {
	client := &scriptedClient{linkErr: errors.New("HTTP 500 sin clasificar")}
	svc, store := newScriptedEnv(t, client)
	ctx := context.Background()

	if _, err := svc.AddLink(ctx, "OMNIFY-2", "doc", "https://x"); err == nil {
		t.Fatal("si Plane falla, asociar un enlace debe fallar")
	}
	if links, _ := store.ListLinks(ctx, "OMNIFY-2"); len(links) != 0 {
		t.Errorf("no debe quedar un enlace local que no existe en Plane: %+v", links)
	}

	client.linkErr = nil
	link, err := svc.AddLink(ctx, "OMNIFY-2", "doc", "https://x")
	if err != nil || link.ID != "link-remote-1" {
		t.Fatalf("AddLink falló: %+v, %v", link, err)
	}
	client.linkErr = errors.New("HTTP 500")
	if err := svc.DeleteLink(ctx, "OMNIFY-2", link.ID); err == nil {
		t.Fatal("si Plane falla, eliminar el enlace debe fallar")
	}
	if links, _ := store.ListLinks(ctx, "OMNIFY-2"); len(links) != 1 {
		t.Errorf("la copia local solo se borra cuando Plane lo confirma: %d enlaces", len(links))
	}

	// Sin conexión (sin credenciales) tampoco se simula un éxito.
	offline, _ := newScriptedEnv(t, nil)
	if _, err := offline.AddLink(ctx, "OMNIFY-2", "doc", "https://x"); err == nil {
		t.Error("sin conexión, asociar un enlace debe fallar a la vista")
	}
}

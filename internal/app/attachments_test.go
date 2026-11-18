package app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/allostasis/alo/internal/domain/issue"
)

const (
	oldAtt   = "aaaaaaaa-0000-0000-0000-000000000001"
	staleAtt = "aaaaaaaa-0000-0000-0000-000000000002"
)

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAddAttachments_UploadsRealContentAndCaches(t *testing.T) {
	client := &scriptedClient{}
	svc, store := newScriptedEnv(t, client)
	ctx := context.Background()

	got, err := svc.AddAttachments(ctx, "OMNIFY-2", []string{writeFile(t, "a.txt", "hola"), writeFile(t, "b.md", "# doc")})
	if err != nil || len(got) != 2 {
		t.Fatalf("AddAttachments falló: %v (%d)", err, len(got))
	}
	if want := []string{"a.txt:hola", "b.md:# doc"}; !reflect.DeepEqual(client.uploads, want) {
		t.Errorf("subidas = %v, esperado %v (contenido real, no simulado)", client.uploads, want)
	}
	if got[0].SizeBytes != 4 {
		t.Errorf("el tamaño debe ser el real del archivo, no un valor fijo: %d", got[0].SizeBytes)
	}
	if cached, _ := store.ListAttachments(ctx, "OMNIFY-2"); len(cached) != 2 {
		t.Errorf("copia local = %d, esperado 2", len(cached))
	}
}

func TestAddAttachments_ValidatesEverythingBeforeTouchingPlane(t *testing.T) {
	client := &scriptedClient{}
	svc, _ := newScriptedEnv(t, client)
	ctx := context.Background()
	good := writeFile(t, "ok.txt", "x")

	cases := map[string][]string{
		"extensión no admitida": {good, writeFile(t, "dump.sql", "select 1")},
		"archivo inexistente":   {good, filepath.Join(t.TempDir(), "no-existe.txt")},
		"es un directorio":      {good, t.TempDir()},
	}
	for name, paths := range cases {
		_, err := svc.AddAttachments(ctx, "OMNIFY-2", paths)
		var invalid *issue.InvalidAttachmentError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: esperado *InvalidAttachmentError, obtuvo %v", name, err)
		}
		if len(client.calls) != 0 {
			t.Errorf("%s: no debe contactarse a Plane si algún archivo es inválido (calls=%v)", name, client.calls)
		}
	}
	if _, err := svc.AddAttachments(ctx, "OMNIFY-2", nil); err == nil {
		t.Error("sin archivos debe fallar")
	}
}

func TestAddAttachments_RequiresPlane_NeverQueuesNorSimulates(t *testing.T) {
	ctx := context.Background()
	file := writeFile(t, "a.txt", "x")

	// Tarea que aún no existe en Plane.
	client := &scriptedClient{}
	svc, store := newScriptedEnv(t, client)
	_ = store.SaveIssue(ctx, &issue.WorkItem{ID: "local-9", Key: "OMNIFY-LOCAL-9", ProjectID: "proj-1", Title: "x"})
	_, err := svc.AddAttachments(ctx, "OMNIFY-LOCAL-9", []string{file})
	var pre *issue.PreconditionError
	if !errors.As(err, &pre) || pre.Hint == "" {
		t.Fatalf("esperado *PreconditionError con pista, obtuvo %v", err)
	}

	// Sin credenciales: error explícito, nada en outbox, nada en la copia local.
	offline, offStore := newScriptedEnv(t, nil)
	if _, err := offline.AddAttachments(ctx, "OMNIFY-2", []string{file}); err == nil || !strings.Contains(err.Error(), "conexión") {
		t.Fatalf("sin conexión debe fallar a la vista: %v", err)
	}
	if outbox, _ := offStore.ListOutbox(ctx, true); len(outbox) != 0 {
		t.Errorf("un adjunto no se encola (outbox = %d)", len(outbox))
	}
	if cached, _ := offStore.ListAttachments(ctx, "OMNIFY-2"); len(cached) != 0 {
		t.Errorf("no debe quedar un registro simulado: %+v", cached)
	}
}

func TestAddAttachments_PartialFailureReportsWhatWasUploaded(t *testing.T) {
	client := &scriptedClient{uploadErr: &issue.TransientError{Status: 503, Operation: "subir archivo"}, uploadFailName: "b.txt"}
	svc, store := newScriptedEnv(t, client)
	ctx := context.Background()

	uploaded, err := svc.AddAttachments(ctx, "OMNIFY-2", []string{writeFile(t, "a.txt", "1"), writeFile(t, "b.txt", "2"), writeFile(t, "c.txt", "3")})
	if err == nil {
		t.Fatal("debe fallar")
	}
	if len(uploaded) != 1 || uploaded[0].Name != "a.txt" {
		t.Errorf("debe devolver lo ya subido para reportarlo: %+v", uploaded)
	}
	if issue.ClassifyRemoteError(err) != issue.RemoteErrTransient {
		t.Errorf("la clasificación del fallo debe conservarse: %v", err)
	}
	if len(client.calls) != 2 {
		t.Errorf("debe detenerse en el primer fallo (calls=%v)", client.calls)
	}
	if outbox, _ := store.ListOutbox(ctx, true); len(outbox) != 0 {
		t.Errorf("un fallo transitorio de subida no se encola: %d", len(outbox))
	}
}

func TestListAttachments_ReconcilesFallsBackAndFailsLoud(t *testing.T) {
	ctx := context.Background()
	newest := issue.Attachment{ID: oldAtt, Name: "nuevo.txt", SizeBytes: 3}

	t.Run("Plane responde: reconcilia y elimina lo borrado", func(t *testing.T) {
		client := &scriptedClient{listItems: []issue.Attachment{newest}, listComplete: true}
		svc, store := newScriptedEnv(t, client)
		_ = store.AddAttachment(ctx, issue.Attachment{ID: staleAtt, TicketID: "OMNIFY-2", IssueID: "iss-2", Name: "borrado.txt"})

		l, err := svc.ListAttachments(ctx, "OMNIFY-2")
		if err != nil || l.Source != "api" || !l.Complete || len(l.Items) != 1 || l.Items[0].TicketID != "OMNIFY-2" {
			t.Fatalf("listado inesperado: %+v, %v", l, err)
		}
		if cached, _ := store.ListAttachments(ctx, "OMNIFY-2"); len(cached) != 1 || cached[0].ID != oldAtt {
			t.Errorf("la copia debe reflejar Plane (sin el borrado): %+v", cached)
		}
	})

	t.Run("lista incompleta: no se descarta nada y se avisa", func(t *testing.T) {
		client := &scriptedClient{listItems: []issue.Attachment{newest}, listComplete: false}
		svc, store := newScriptedEnv(t, client)
		_ = store.AddAttachment(ctx, issue.Attachment{ID: staleAtt, TicketID: "OMNIFY-2", IssueID: "iss-2", Name: "quizá-existe.txt"})
		l, _ := svc.ListAttachments(ctx, "OMNIFY-2")
		if l.Complete || l.Note == "" {
			t.Errorf("debe avisar que está incompleta: %+v", l)
		}
		if cached, _ := store.ListAttachments(ctx, "OMNIFY-2"); len(cached) != 2 {
			t.Errorf("con lista incompleta no se puede asumir que algo fue eliminado: %d", len(cached))
		}
	})

	t.Run("fallo transitorio: copia local marcada como tal", func(t *testing.T) {
		client := &scriptedClient{listErr: &issue.TransientError{Status: 503, Operation: "listar adjuntos"}}
		svc, store := newScriptedEnv(t, client)
		_ = store.AddAttachment(ctx, issue.Attachment{ID: oldAtt, TicketID: "OMNIFY-2", IssueID: "iss-2", Name: "copia.txt"})
		l, err := svc.ListAttachments(ctx, "OMNIFY-2")
		if err != nil || l.Source != "cache" || l.Note == "" || len(l.Items) != 1 {
			t.Errorf("debe devolver la copia declarando su procedencia: %+v, %v", l, err)
		}
	})

	t.Run("credenciales: se muestra, no se esconde tras la copia", func(t *testing.T) {
		client := &scriptedClient{listErr: &issue.AuthError{Status: 401, Operation: "listar adjuntos"}}
		svc, _ := newScriptedEnv(t, client)
		if _, err := svc.ListAttachments(ctx, "OMNIFY-2"); err == nil {
			t.Error("un 401 no debe disfrazarse de copia local")
		}
	})
}

func TestDeleteAttachment(t *testing.T) {
	ctx := context.Background()
	seed := func(store interface {
		AddAttachment(context.Context, issue.Attachment) error
	}) {
		_ = store.AddAttachment(ctx, issue.Attachment{ID: oldAtt, TicketID: "OMNIFY-2", IssueID: "iss-2", Name: "x.txt"})
	}

	t.Run("id inválido: ni toca la red", func(t *testing.T) {
		client := &scriptedClient{}
		svc, _ := newScriptedEnv(t, client)
		if err := svc.DeleteAttachment(ctx, "OMNIFY-2", "no-es-uuid"); err == nil || len(client.calls) != 0 {
			t.Errorf("err=%v calls=%v", err, client.calls)
		}
	})
	t.Run("éxito: Plane y copia local", func(t *testing.T) {
		client := &scriptedClient{}
		svc, store := newScriptedEnv(t, client)
		seed(store)
		if err := svc.DeleteAttachment(ctx, "OMNIFY-2", oldAtt); err != nil || len(client.deleted) != 1 {
			t.Fatalf("err=%v deleted=%v", err, client.deleted)
		}
		if cached, _ := store.ListAttachments(ctx, "OMNIFY-2"); len(cached) != 0 {
			t.Errorf("la copia local debe borrarse tras la confirmación: %+v", cached)
		}
	})
	t.Run("Plane falla: la copia local se conserva", func(t *testing.T) {
		client := &scriptedClient{deleteErr: &issue.TransientError{Status: 502, Operation: "eliminar adjunto"}}
		svc, store := newScriptedEnv(t, client)
		seed(store)
		if err := svc.DeleteAttachment(ctx, "OMNIFY-2", oldAtt); err == nil {
			t.Fatal("debe fallar")
		}
		if cached, _ := store.ListAttachments(ctx, "OMNIFY-2"); len(cached) != 1 {
			t.Errorf("sin confirmación de Plane no se borra la copia: %+v", cached)
		}
	})
	t.Run("404: informa y limpia la copia obsoleta", func(t *testing.T) {
		client := &scriptedClient{deleteErr: &issue.RejectedError{Status: 404, Operation: "eliminar adjunto"}}
		svc, store := newScriptedEnv(t, client)
		seed(store)
		if err := svc.DeleteAttachment(ctx, "OMNIFY-2", oldAtt); err == nil {
			t.Fatal("un adjunto inexistente se informa")
		}
		if cached, _ := store.ListAttachments(ctx, "OMNIFY-2"); len(cached) != 0 {
			t.Errorf("la copia obsoleta debe limpiarse: %+v", cached)
		}
	})
}

func TestReplaceAttachment(t *testing.T) {
	ctx := context.Background()
	file := func(t *testing.T) string { return writeFile(t, "nuevo.txt", "v2") }

	t.Run("sube el nuevo y solo después elimina el anterior", func(t *testing.T) {
		client := &scriptedClient{listItems: []issue.Attachment{{ID: oldAtt, Name: "viejo.txt"}}, listComplete: true}
		svc, _ := newScriptedEnv(t, client)
		att, err := svc.ReplaceAttachment(ctx, "OMNIFY-2", oldAtt, file(t))
		if err != nil || att == nil {
			t.Fatalf("ReplaceAttachment falló: %v", err)
		}
		if want := []string{"list", "upload:nuevo.txt", "delete:" + oldAtt}; !reflect.DeepEqual(client.calls, want) {
			t.Errorf("orden de llamadas = %v, esperado %v", client.calls, want)
		}
	})
	t.Run("anterior inexistente: no sube nada", func(t *testing.T) {
		client := &scriptedClient{listItems: []issue.Attachment{{ID: staleAtt}}, listComplete: true}
		svc, _ := newScriptedEnv(t, client)
		_, err := svc.ReplaceAttachment(ctx, "OMNIFY-2", oldAtt, file(t))
		var pre *issue.PreconditionError
		if !errors.As(err, &pre) {
			t.Fatalf("esperado *PreconditionError, obtuvo %v", err)
		}
		if len(client.uploads) != 0 {
			t.Error("un id equivocado no debe dejar un duplicado subido")
		}
	})
	t.Run("falla la subida: el anterior queda intacto", func(t *testing.T) {
		client := &scriptedClient{listItems: []issue.Attachment{{ID: oldAtt}}, listComplete: true, uploadErr: &issue.TransientError{Status: 503}}
		svc, _ := newScriptedEnv(t, client)
		if _, err := svc.ReplaceAttachment(ctx, "OMNIFY-2", oldAtt, file(t)); err == nil {
			t.Fatal("debe fallar")
		}
		if len(client.deleted) != 0 {
			t.Error("si el nuevo no se subió, el anterior no se toca")
		}
	})
	t.Run("falla eliminar el anterior: se informa de ambos", func(t *testing.T) {
		client := &scriptedClient{listItems: []issue.Attachment{{ID: oldAtt}}, listComplete: true, deleteErr: &issue.TransientError{Status: 502}}
		svc, _ := newScriptedEnv(t, client)
		att, err := svc.ReplaceAttachment(ctx, "OMNIFY-2", oldAtt, file(t))
		if att == nil || err == nil || !strings.Contains(err.Error(), oldAtt) || !strings.Contains(err.Error(), att.ID) {
			t.Errorf("debe devolver el nuevo y un error que nombre ambos: att=%+v err=%v", att, err)
		}
	})
}

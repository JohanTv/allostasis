package app_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/allostasis/alo/internal/domain/issue"
)

func TestListLinks_ReconcilesFallsBackAndFailsLoud(t *testing.T) {
	ctx := context.Background()
	newest := issue.Link{
		ID:        "lnk-new",
		IssueID:   "iss-2",
		TicketID:  "OMNIFY-2",
		Title:     "PR 10",
		URL:       "https://github.com/org/repo/pull/10",
		CreatedAt: time.Now(),
	}

	t.Run("Plane responde: reconcilia y elimina lo borrado", func(t *testing.T) {
		client := &scriptedClient{listLinksItems: []issue.Link{newest}}
		svc, store := newScriptedEnv(t, client)
		_ = store.AddLink(ctx, issue.Link{
			ID:        "lnk-stale",
			IssueID:   "iss-2",
			TicketID:  "OMNIFY-2",
			Title:     "PR vieja borrada en Plane",
			URL:       "https://github.com/org/repo/pull/1",
			CreatedAt: time.Now(),
		})

		l, err := svc.ListLinks(ctx, "OMNIFY-2")
		if err != nil || l.Source != "api" || !l.Complete || len(l.Items) != 1 || l.Items[0].TicketID != "OMNIFY-2" {
			t.Fatalf("listado inesperado: %+v, %v", l, err)
		}
		if cached, _ := store.ListLinks(ctx, "OMNIFY-2"); len(cached) != 1 || cached[0].ID != "lnk-new" {
			t.Errorf("la copia debe reflejar Plane (sin el borrado): %+v", cached)
		}
	})

	t.Run("fallo transitorio: copia local marcada como tal", func(t *testing.T) {
		client := &scriptedClient{listLinksErr: &issue.TransientError{Status: 503, Operation: "listar enlaces"}}
		svc, store := newScriptedEnv(t, client)
		_ = store.AddLink(ctx, issue.Link{
			ID:        "lnk-cached",
			IssueID:   "iss-2",
			TicketID:  "OMNIFY-2",
			Title:     "Copia local",
			URL:       "https://github.com/org/repo/pull/2",
			CreatedAt: time.Now(),
		})

		l, err := svc.ListLinks(ctx, "OMNIFY-2")
		if err != nil || l.Source != "cache" || l.Complete || l.Note == "" || len(l.Items) != 1 {
			t.Fatalf("debe devolver la copia declarando su procedencia: %+v, %v", l, err)
		}
		if l.Items[0].ID != "lnk-cached" {
			t.Errorf("debe devolver el item del caché: %+v", l.Items[0])
		}
	})

	t.Run("sin conexión o tarea local: copia local marcada como tal", func(t *testing.T) {
		svc, store := newScriptedEnv(t, nil) // client nil
		_ = store.AddLink(ctx, issue.Link{
			ID:        "lnk-local",
			IssueID:   "iss-2",
			TicketID:  "OMNIFY-2",
			Title:     "Enlace local",
			URL:       "https://example.com",
			CreatedAt: time.Now(),
		})

		l, err := svc.ListLinks(ctx, "OMNIFY-2")
		if err != nil || l.Source != "cache" || l.Complete || l.Note == "" || len(l.Items) != 1 {
			t.Fatalf("debe devolver copia local con nota: %+v, %v", l, err)
		}
	})

	t.Run("credenciales: se muestra, no se esconde tras la copia", func(t *testing.T) {
		client := &scriptedClient{listLinksErr: &issue.AuthError{Status: 401, Operation: "listar enlaces"}}
		svc, store := newScriptedEnv(t, client)
		_ = store.AddLink(ctx, issue.Link{
			ID:        "lnk-cached",
			IssueID:   "iss-2",
			TicketID:  "OMNIFY-2",
			Title:     "Enlace",
			URL:       "https://example.com",
			CreatedAt: time.Now(),
		})

		if _, err := svc.ListLinks(ctx, "OMNIFY-2"); err == nil {
			t.Error("un 401 no debe disfrazarse de copia local")
		}
	})

	t.Run("error desconocido: se muestra, no se esconde tras la copia", func(t *testing.T) {
		client := &scriptedClient{listLinksErr: fmt.Errorf("fallo no clasificado")}
		svc, store := newScriptedEnv(t, client)
		_ = store.AddLink(ctx, issue.Link{
			ID:        "lnk-cached",
			IssueID:   "iss-2",
			TicketID:  "OMNIFY-2",
			Title:     "Enlace",
			URL:       "https://example.com",
			CreatedAt: time.Now(),
		})

		if _, err := svc.ListLinks(ctx, "OMNIFY-2"); err == nil {
			t.Error("un error no clasificado debe propagarse, no ocultarse tras la copia")
		}
	})
}

package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/JohanTv/allostasis/internal/domain/issue"
)

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
	createdAt := remote.CreatedAt
	if createdAt.IsZero() {
		createdAt = s.now()
	}
	link := &issue.Link{ID: remote.ID, IssueID: item.ID, TicketID: item.Key, Title: title, URL: linkURL, CreatedAt: createdAt}
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

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/allostasis/alo/internal/domain/issue"
)

// Los adjuntos viven en Plane: no hay cola outbox para ellos (un archivo puede cambiar o desaparecer
// antes de la sincronización), así que sin conexión fallan a la vista en vez de simular un éxito. El
// registro local es solo una copia para consultar sin red; `ListAttachments` la reconcilia con Plane.

type attachmentFile struct {
	path string
	name string
	size int64
}

// validateAttachmentFiles revisa todos los archivos antes de contactar a Plane, para que una subida
// múltiple no quede a medias por un archivo inválido.
func validateAttachmentFiles(paths []string) ([]attachmentFile, error) {
	if len(paths) == 0 {
		return nil, &issue.PreconditionError{Message: "indica al menos un archivo a adjuntar"}
	}
	files := make([]attachmentFile, 0, len(paths))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, &issue.InvalidAttachmentError{Path: p, Reason: "no existe o no se puede leer"}
		}
		if !info.Mode().IsRegular() {
			return nil, &issue.InvalidAttachmentError{Path: p, Reason: "no es un archivo regular"}
		}
		if err := issue.ValidateAttachmentFile(p, info.Size()); err != nil {
			return nil, err
		}
		files = append(files, attachmentFile{path: p, name: filepath.Base(p), size: info.Size()})
	}
	return files, nil
}

// uploadAttachment sube un archivo ya validado. Sin conexión o ante cualquier fallo retorna error.
func (s *TrackerService) uploadAttachment(ctx context.Context, item *issue.WorkItem, f attachmentFile) (*issue.Attachment, error) {
	var att *issue.Attachment
	_, err := s.dispatch(ctx, remoteMutation{
		Name: "subir adjunto",
		Send: func(ctx context.Context) error {
			fh, err := os.Open(f.path)
			if err != nil {
				return fmt.Errorf("no se pudo abrir '%s': %w", f.path, err)
			}
			defer fh.Close()
			uploaded, err := s.client.UploadAttachmentRemote(ctx, item.ProjectID, item.ID, issue.AttachmentUpload{
				Name:        f.name,
				ContentType: issue.AttachmentContentType(f.name),
				Size:        f.size,
				Content:     fh,
			})
			if err != nil {
				return err
			}
			if uploaded == nil {
				return fmt.Errorf("Plane no devolvió el adjunto subido")
			}
			att = uploaded
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error subiendo '%s': %w", f.path, err)
	}
	att.TicketID = item.Key
	_ = s.store.AddAttachment(ctx, *att) // copia local; ListAttachments la reconcilia con Plane
	return att, nil
}

// AddAttachments sube uno o más archivos a una tarea. Valida todos antes de tocar Plane; si uno falla
// a mitad, retorna los ya subidos junto con el error para que quien llama los reporte.
func (s *TrackerService) AddAttachments(ctx context.Context, key string, paths []string) ([]issue.Attachment, error) {
	item, err := s.remoteTarget(ctx, key)
	if err != nil {
		return nil, err
	}
	files, err := validateAttachmentFiles(paths)
	if err != nil {
		return nil, err
	}
	uploaded := make([]issue.Attachment, 0, len(files))
	for _, f := range files {
		att, err := s.uploadAttachment(ctx, item, f)
		if err != nil {
			return uploaded, err
		}
		uploaded = append(uploaded, *att)
	}
	return uploaded, nil
}

// ListAttachments lista los adjuntos de una tarea. Plane es la fuente de verdad: si responde, la copia local
// se reconcilia (se descartan los eliminados). Si Plane no responde por un fallo transitorio, se devuelve la
// copia local marcada como tal; cualquier otro fallo se muestra.
func (s *TrackerService) ListAttachments(ctx context.Context, key string) (*issue.AttachmentListing, error) {
	if s.store == nil {
		return nil, fmt.Errorf("almacenamiento no inicializado")
	}
	item, err := s.store.GetIssue(ctx, key)
	if err != nil {
		return nil, err
	}
	fromCache := func(note string) (*issue.AttachmentListing, error) {
		items, err := s.store.ListAttachments(ctx, item.Key)
		if err != nil {
			return nil, err
		}
		return &issue.AttachmentListing{Items: items, Source: "cache", Complete: false, Note: note}, nil
	}

	if s.client == nil || item.ProjectID == "" || item.ID == "" || strings.HasPrefix(item.ID, "local-") {
		return fromCache("sin conexión con Plane o tarea aún local: copia local, puede estar desactualizada")
	}
	remote, complete, err := s.client.ListAttachmentsRemote(ctx, item.ProjectID, item.ID)
	if err != nil {
		if issue.ClassifyRemoteError(err) == issue.RemoteErrTransient {
			return fromCache(fmt.Sprintf("Plane no respondió (%v): copia local, puede estar desactualizada", err))
		}
		return nil, err
	}

	seen := make(map[string]bool, len(remote))
	for i := range remote {
		remote[i].TicketID = item.Key
		seen[remote[i].ID] = true
		_ = s.store.AddAttachment(ctx, remote[i])
	}
	listing := &issue.AttachmentListing{Items: remote, Source: "api", Complete: complete}
	if complete {
		if cached, err := s.store.ListAttachments(ctx, item.Key); err == nil {
			for _, c := range cached {
				if !seen[c.ID] {
					_ = s.store.DeleteAttachment(ctx, c.ID)
				}
			}
		}
	} else {
		listing.Note = "Plane anuncia más páginas de adjuntos: la lista está incompleta"
	}
	return listing, nil
}

// DeleteAttachment elimina un adjunto en Plane y, solo si Plane lo confirma, de la copia local.
func (s *TrackerService) DeleteAttachment(ctx context.Context, key, attachmentID string) error {
	if err := issue.ValidateAttachmentID(attachmentID); err != nil {
		return err
	}
	item, err := s.remoteTarget(ctx, key)
	if err != nil {
		return err
	}
	return s.deleteAttachment(ctx, item, attachmentID)
}

func (s *TrackerService) deleteAttachment(ctx context.Context, item *issue.WorkItem, attachmentID string) error {
	_, err := s.dispatch(ctx, remoteMutation{
		Name: "eliminar adjunto",
		Send: func(ctx context.Context) error {
			return s.client.DeleteAttachmentRemote(ctx, item.ProjectID, item.ID, attachmentID)
		},
	})
	var rejected *issue.RejectedError
	if err == nil || (errors.As(err, &rejected) && rejected.Status == 404) {
		_ = s.store.DeleteAttachment(ctx, attachmentID) // ya no existe en Plane: se limpia la copia local
	}
	return err
}

// ReplaceAttachment sustituye un adjunto: comprueba que el anterior existe, sube el nuevo y solo entonces
// elimina el anterior. Si la eliminación falla retorna el adjunto nuevo junto con el error (ambos existen).
func (s *TrackerService) ReplaceAttachment(ctx context.Context, key, attachmentID, path string) (*issue.Attachment, error) {
	if err := issue.ValidateAttachmentID(attachmentID); err != nil {
		return nil, err
	}
	item, err := s.remoteTarget(ctx, key)
	if err != nil {
		return nil, err
	}
	files, err := validateAttachmentFiles([]string{path})
	if err != nil {
		return nil, err
	}

	// Que el anterior exista, antes de subir nada: así un id equivocado no deja un duplicado.
	if s.client == nil {
		return nil, &issue.PreconditionError{Message: "reemplazar adjunto requiere conexión con Plane: PLANE_API_KEY no está configurada", Hint: "Configura PLANE_BASE_URL, PLANE_API_KEY y PLANE_WORKSPACE_SLUG (comprueba con 'alo ticket whoami')"}
	}
	current, complete, err := s.client.ListAttachmentsRemote(ctx, item.ProjectID, item.ID)
	if err != nil {
		return nil, fmt.Errorf("no se pudo comprobar el adjunto anterior: %w", err)
	}
	found := false
	for _, a := range current {
		found = found || a.ID == attachmentID
	}
	if !found && complete {
		return nil, &issue.PreconditionError{
			Message: fmt.Sprintf("el adjunto %s no existe en %s", attachmentID, item.Key),
			Hint:    "Consulta los identificadores con 'alo ticket attachment list " + item.Key + "'",
		}
	}

	att, err := s.uploadAttachment(ctx, item, files[0])
	if err != nil {
		return nil, err
	}
	if err := s.deleteAttachment(ctx, item, attachmentID); err != nil {
		return att, fmt.Errorf("el adjunto nuevo %s se subió, pero no se pudo eliminar el anterior %s: %w", att.ID, attachmentID, err)
	}
	return att, nil
}

package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/JohanTv/allostasis/internal/domain/issue"
)

// Disposition dice qué pasó con una mutación remota. Quien la consume debe reportarla: una mutación
// "queued" existe solo en local hasta el próximo `alo sync`, y el usuario tiene que saberlo.
type Disposition string

const (
	// Applied: Plane aceptó la mutación.
	Applied Disposition = "applied"
	// Queued: Plane no estaba disponible; la mutación quedó en el outbox.
	Queued Disposition = "queued"
)

// remoteMutation describe una mutación hacia Plane. Es el único camino para decidir qué se hace con su fallo.
type remoteMutation struct {
	// Name identifica la operación en los mensajes de error.
	Name string
	// Send intenta la mutación en Plane.
	Send func(ctx context.Context) error
	// Enqueue la deja en el outbox. nil = la operación no admite diferirse (p. ej. subir un archivo):
	// un fallo transitorio se muestra al usuario en vez de encolarse.
	Enqueue func(ctx context.Context) error
}

// dispatch aplica la política única de mutaciones remotas (ADR 0032). Sin cliente remoto (modo sin conexión
// explícito) la mutación se encola si admite diferirse. Con cliente, según issue.ClassifyRemoteError:
//
//	éxito                  → Applied
//	rechazo de datos       → error (reintentar no lo arregla)
//	transitorio            → Queued si admite diferirse; si no, error
//	credenciales o DESCONOCIDO → error
//
// El valor por defecto es mostrar el error: lo que una nueva operación no clasifique nunca se encola
// ni se descarta en silencio.
func (s *TrackerService) dispatch(ctx context.Context, m remoteMutation) (Disposition, error) {
	if s.client == nil {
		if m.Enqueue == nil {
			return "", &issue.PreconditionError{
				Message: fmt.Sprintf("%s requiere conexión con Plane y no se puede diferir: PLANE_API_KEY no está configurada", m.Name),
				Hint:    "Configura PLANE_BASE_URL, PLANE_API_KEY y PLANE_WORKSPACE_SLUG (comprueba con 'alo ticket whoami')",
			}
		}
		if err := m.Enqueue(ctx); err != nil {
			return "", fmt.Errorf("%s: no se pudo encolar: %w", m.Name, err)
		}
		return Queued, nil
	}

	err := m.Send(ctx)
	if err == nil {
		return Applied, nil
	}
	if issue.ClassifyRemoteError(err) == issue.RemoteErrTransient && m.Enqueue != nil {
		if qerr := m.Enqueue(ctx); qerr != nil {
			return "", fmt.Errorf("%s: Plane no respondió (%v) y tampoco se pudo encolar: %w", m.Name, err, qerr)
		}
		return Queued, nil
	}
	return "", err
}

// remoteTarget resuelve la tarea y exige que ya exista en Plane: adjuntos y enlaces no se pueden diferir.
func (s *TrackerService) remoteTarget(ctx context.Context, key string) (*issue.WorkItem, error) {
	if s.store == nil {
		return nil, fmt.Errorf("almacenamiento no inicializado")
	}
	item, err := s.store.GetIssue(ctx, key)
	if err != nil {
		return nil, err
	}
	if item.ProjectID == "" || item.ID == "" || strings.HasPrefix(item.ID, "local-") {
		return nil, &issue.PreconditionError{
			Message: fmt.Sprintf("la tarea %s aún no existe en Plane (está pendiente de sincronizar)", item.Key),
			Hint:    "Ejecuta 'alo sync' y reintenta",
		}
	}
	return item, nil
}

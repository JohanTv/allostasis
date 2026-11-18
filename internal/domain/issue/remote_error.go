package issue

import (
	"errors"
	"fmt"
	"strings"
)

// RemoteErrorKind clasifica el fallo de una mutación remota para decidir qué se hace con ella.
// El valor cero es RemoteErrUnknown a propósito: lo que nadie clasificó se muestra, nunca se oculta.
type RemoteErrorKind int

const (
	// RemoteErrUnknown: error sin clasificar. Se muestra al usuario; no se encola ni se ignora.
	RemoteErrUnknown RemoteErrorKind = iota
	// RemoteErrRejected: Plane rechazó los datos (4xx). Reintentar no lo arregla.
	RemoteErrRejected
	// RemoteErrAuth: credenciales o permisos (401/403). Encolarlo ocultaría un problema de configuración.
	RemoteErrAuth
	// RemoteErrTransient: red, timeout, 429 o 5xx. Reintentable más tarde.
	RemoteErrTransient
)

func (k RemoteErrorKind) String() string {
	switch k {
	case RemoteErrRejected:
		return "rejected"
	case RemoteErrAuth:
		return "auth"
	case RemoteErrTransient:
		return "transient"
	default:
		return "unknown"
	}
}

// UnknownLabelError indica etiquetas que no existen en el proyecto (error de entrada).
type UnknownLabelError struct {
	Names []string
	Valid []string
}

func (e *UnknownLabelError) Error() string {
	return fmt.Sprintf("etiqueta(s) desconocida(s): %s (válidas: %s)", strings.Join(e.Names, ", "), strings.Join(e.Valid, ", "))
}

// UnknownStateError indica un estado que no existe en el proyecto (error de entrada). Sin este error el
// cambio de estado se omitiría en silencio, porque Plane lo identifica por ID.
type UnknownStateError struct {
	Name  string
	Valid []string
}

func (e *UnknownStateError) Error() string {
	return fmt.Sprintf("estado desconocido: %s (válidos: %s)", e.Name, strings.Join(e.Valid, ", "))
}

// PreconditionError indica que la operación no puede hacerse todavía (p. ej. la tarea aún no existe en Plane).
// Es un error de uso: se muestra con su pista y nunca se encola.
type PreconditionError struct {
	Message string
	Hint    string
}

func (e *PreconditionError) Error() string { return e.Message }

// InvalidAttachmentError indica un archivo que no puede adjuntarse (error de entrada, detectado antes de tocar Plane).
type InvalidAttachmentError struct {
	Path   string
	Reason string
	Hint   string
}

func (e *InvalidAttachmentError) Error() string {
	return fmt.Sprintf("'%s' no se puede adjuntar: %s", e.Path, e.Reason)
}

// RejectedError indica que Plane rechazó la mutación por datos inválidos (400, 404, 409, 422).
type RejectedError struct {
	Status    int
	Operation string
	Detail    string
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("Plane retornó HTTP %d al %s: %s", e.Status, e.Operation, e.Detail)
}

// AuthError indica que Plane rechazó las credenciales o los permisos (401/403).
type AuthError struct {
	Status    int
	Operation string
	Detail    string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("Plane retornó HTTP %d al %s (credenciales o permisos): %s", e.Status, e.Operation, e.Detail)
}

// TransientError indica un fallo reintentable. Status es 0 cuando el fallo fue de red o de timeout.
type TransientError struct {
	Operation string
	Status    int
	Detail    string
	Cause     error
}

func (e *TransientError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("error de red al %s en Plane: %v", e.Operation, e.Cause)
	}
	return fmt.Sprintf("Plane retornó HTTP %d al %s: %s", e.Status, e.Operation, e.Detail)
}

func (e *TransientError) Unwrap() error { return e.Cause }

// ClassifyRemoteError traduce un error de mutación remota a su clase. Todo lo que no sea un error
// tipado de este paquete es RemoteErrUnknown.
func ClassifyRemoteError(err error) RemoteErrorKind {
	var unknownLabel *UnknownLabelError
	var unknownState *UnknownStateError
	var invalidAttachment *InvalidAttachmentError
	var precondition *PreconditionError
	var rejected *RejectedError
	var auth *AuthError
	var transient *TransientError
	switch {
	case err == nil:
		return RemoteErrUnknown
	case errors.As(err, &unknownLabel), errors.As(err, &unknownState), errors.As(err, &invalidAttachment), errors.As(err, &precondition), errors.As(err, &rejected):
		return RemoteErrRejected
	case errors.As(err, &auth):
		return RemoteErrAuth
	case errors.As(err, &transient):
		return RemoteErrTransient
	}
	return RemoteErrUnknown
}

// IsInputError reporta si err es un rechazo por datos del usuario: se propaga y nunca se encola.
func IsInputError(err error) bool {
	return err != nil && ClassifyRemoteError(err) == RemoteErrRejected
}

package issue

import (
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"regexp"
	"strings"
)

// MaxAttachmentBytes es el tamaño máximo de un adjunto (límite de Plane CE).
const MaxAttachmentBytes int64 = 50 * 1024 * 1024

// unsupportedAttachmentExtensions son tipos que Plane rechaza como adjunto.
var unsupportedAttachmentExtensions = map[string]bool{".sql": true}

// AttachmentUpload describe un archivo a subir. Content se consume una sola vez.
type AttachmentUpload struct {
	Name        string
	ContentType string
	Size        int64
	Content     io.Reader
}

// AttachmentListing es el resultado de listar adjuntos, con la procedencia de los datos explícita:
// "api" (Plane, autoritativo) o "cache" (copia local, posiblemente desactualizada, con Note del motivo).
type AttachmentListing struct {
	Items    []Attachment `json:"items"`
	Source   string       `json:"source"`
	Complete bool         `json:"complete"`
	Note     string       `json:"note,omitempty"`
}

var attachmentIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ValidateAttachmentID rechaza, antes de tocar la red, un identificador que no es el UUID de un adjunto.
func ValidateAttachmentID(id string) error {
	if !attachmentIDPattern.MatchString(id) {
		return &InvalidAttachmentError{
			Path:   id,
			Reason: "no es un UUID de adjunto",
			Hint:   "Consulta los identificadores con 'alo ticket attachment list <TICKET>'",
		}
	}
	return nil
}

// ValidateAttachmentFile valida nombre y tamaño de un archivo local antes de contactar a Plane,
// para que una subida múltiple no quede a medias por un archivo inválido.
func ValidateAttachmentFile(path string, size int64) error {
	if size > MaxAttachmentBytes {
		return &InvalidAttachmentError{Path: path, Reason: fmt.Sprintf("supera el máximo de %d MB", MaxAttachmentBytes/(1024*1024))}
	}
	ext := strings.ToLower(filepath.Ext(path))
	if unsupportedAttachmentExtensions[ext] {
		return &InvalidAttachmentError{
			Path:   path,
			Reason: fmt.Sprintf("la extensión %s no es admitida por Plane", ext),
			Hint:   "Convierte o renombra el archivo a .txt o .md para subirlo como adjunto",
		}
	}
	return nil
}

// AttachmentContentType deduce el tipo MIME (sin parámetros) por la extensión; octet-stream si no la conoce.
func AttachmentContentType(name string) string {
	if raw := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); raw != "" {
		if mediaType, _, err := mime.ParseMediaType(raw); err == nil {
			return mediaType
		}
	}
	return "application/octet-stream"
}

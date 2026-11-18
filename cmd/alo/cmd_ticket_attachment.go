package main

import (
	"context"
	"fmt"

	"github.com/allostasis/alo/internal/app"
	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/presentation/errors"
	"github.com/spf13/cobra"
)

// newTicketAttachmentCmd arma `alo ticket attachment add|list|delete|replace`. Los adjuntos viven en Plane:
// requieren conexión y no se encolan (ADR 0033); sin ella fallan a la vista.
func newTicketAttachmentCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "attachment",
		Short: "Gestión de archivos adjuntos de un ticket (add, list, delete, replace)",
		Long: `Sube, lista, elimina y sustituye los archivos adjuntos de un ticket en Plane.

Requieren conexión con Plane y no se encolan en el outbox: sin conexión el comando falla.
Cada archivo se valida antes de contactar a Plane (existe, máx. 50 MB, extensión admitida).`,
	}

	// run abre el servicio, ejecuta fn y deja que un error termine el proceso con su categoría y código.
	run := func(code string, fn func(ctx context.Context, svc *app.TrackerService) error) func(*cobra.Command, []string) {
		return func(cmd *cobra.Command, args []string) {
			svc, store, err := initTrackerService()
			if err != nil {
				errors.EmitErrorWithDetail(err, errors.ErrorDetail{Code: "INIT_FAILED", Category: errors.CategoryInternal}, errors.ExitGeneralError, jsonFlag)
			}
			defer store.Close()
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			if err := fn(ctx, svc); err != nil {
				detail, exitCode := mutationErrorDetail(err, code)
				errors.EmitErrorWithDetail(err, detail, exitCode, jsonFlag)
			}
		}
	}

	add := &cobra.Command{
		Use:   "add <TICKET-ID> <ARCHIVO>...",
		Short: "Subir uno o más archivos al ticket",
		Args:  cobra.MinimumNArgs(2),
	}
	add.Run = func(cmd *cobra.Command, args []string) {
		run("ATTACHMENT_ADD_FAILED", func(ctx context.Context, svc *app.TrackerService) error {
			uploaded, err := svc.AddAttachments(ctx, args[0], args[1:])
			if err != nil {
				// Lo ya subido se reporta: un fallo a mitad no debe ocultar qué sí quedó en Plane.
				if len(uploaded) > 0 {
					if jsonFlag {
						detail, exitCode := mutationErrorDetail(err, "ATTACHMENT_ADD_FAILED")
						detail.Context = map[string]interface{}{"uploaded": uploaded}
						errors.EmitErrorWithDetail(err, detail, exitCode, true)
					}
					for _, a := range uploaded {
						fmt.Printf("✓ Subido antes del fallo: %s (%s) [%s]\n", a.Name, humanBytes(a.SizeBytes), a.ID)
					}
				}
				return err
			}
			if jsonFlag {
				errors.EmitSuccess(map[string]interface{}{"ticket": args[0], "attachments": uploaded}, true)
				return nil
			}
			for _, a := range uploaded {
				fmt.Printf("✓ Adjunto subido a %s: %s (%s) [%s]\n", args[0], a.Name, humanBytes(a.SizeBytes), a.ID)
			}
			return nil
		})(cmd, args)
	}

	list := &cobra.Command{
		Use:   "list <TICKET-ID>",
		Short: "Listar los adjuntos del ticket",
		Args:  cobra.ExactArgs(1),
	}
	list.Run = func(cmd *cobra.Command, args []string) {
		run("ATTACHMENT_LIST_FAILED", func(ctx context.Context, svc *app.TrackerService) error {
			listing, err := svc.ListAttachments(ctx, args[0])
			if err != nil {
				return err
			}
			if listing.Items == nil {
				listing.Items = make([]issue.Attachment, 0)
			}
			if jsonFlag {
				errors.EmitSuccess(map[string]interface{}{"ticket": args[0], "source": listing.Source, "complete": listing.Complete, "note": listing.Note, "attachments": listing.Items}, true)
				return nil
			}
			fmt.Printf("📎 Adjuntos de %s (%d) [fuente: %s]:\n", args[0], len(listing.Items), listing.Source)
			for _, a := range listing.Items {
				fmt.Printf("  • %s (%s) [%s]\n", a.Name, humanBytes(a.SizeBytes), a.ID)
			}
			if listing.Note != "" {
				fmt.Printf("⚠ %s\n", listing.Note)
			}
			return nil
		})(cmd, args)
	}

	del := &cobra.Command{
		Use:   "delete <TICKET-ID> <ATTACHMENT-ID>",
		Short: "Eliminar un adjunto del ticket",
		Args:  cobra.ExactArgs(2),
	}
	del.Run = func(cmd *cobra.Command, args []string) {
		run("ATTACHMENT_DELETE_FAILED", func(ctx context.Context, svc *app.TrackerService) error {
			if err := svc.DeleteAttachment(ctx, args[0], args[1]); err != nil {
				return err
			}
			if jsonFlag {
				errors.EmitSuccess(map[string]string{"status": "deleted", "ticket": args[0], "attachment_id": args[1]}, true)
				return nil
			}
			fmt.Printf("✓ Adjunto %s eliminado de %s.\n", args[1], args[0])
			return nil
		})(cmd, args)
	}

	replace := &cobra.Command{
		Use:   "replace <TICKET-ID> <ATTACHMENT-ID> <ARCHIVO>",
		Short: "Sustituir un adjunto por otro archivo",
		Long:  "Comprueba que el adjunto anterior existe, sube el archivo nuevo y solo entonces elimina el anterior.",
		Args:  cobra.ExactArgs(3),
	}
	replace.Run = func(cmd *cobra.Command, args []string) {
		run("ATTACHMENT_REPLACE_FAILED", func(ctx context.Context, svc *app.TrackerService) error {
			att, err := svc.ReplaceAttachment(ctx, args[0], args[1], args[2])
			if err != nil {
				if att != nil { // el nuevo se subió pero el anterior no se pudo eliminar: existen ambos
					if jsonFlag {
						detail, exitCode := mutationErrorDetail(err, "ATTACHMENT_REPLACE_FAILED")
						detail.Context = map[string]interface{}{"uploaded": att, "not_deleted": args[1]}
						errors.EmitErrorWithDetail(err, detail, exitCode, true)
					}
					fmt.Printf("✓ Subido el adjunto nuevo: %s [%s]\n", att.Name, att.ID)
				}
				return err
			}
			if jsonFlag {
				errors.EmitSuccess(map[string]interface{}{"status": "replaced", "ticket": args[0], "replaced_id": args[1], "attachment": att}, true)
				return nil
			}
			fmt.Printf("✓ Adjunto %s reemplazado por %s (%s) [%s].\n", args[1], att.Name, humanBytes(att.SizeBytes), att.ID)
			return nil
		})(cmd, args)
	}

	root.AddCommand(add, list, del, replace)
	return root
}

func humanBytes(n int64) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%d B", n)
}

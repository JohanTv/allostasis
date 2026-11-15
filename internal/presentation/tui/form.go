package tui

import (
	"context"
	"fmt"
	"strconv"

	"github.com/allostasis/alo/internal/app"
	"github.com/allostasis/alo/internal/domain/ledger"
	"github.com/charmbracelet/huh"
)

// RunLogForm ejecuta un formulario interactivo con Huh para registrar esfuerzo.
func RunLogForm(ctx context.Context, svc *app.CockpitService, defaultTicket string) error {
	var (
		ticketID string = defaultTicket
		duration string = "30"
		bucket   string = "deep_work"
		notes    string
	)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Clave del Ticket").
				Description("Identificador del ticket (ej. OMNIFY-175, OPS-24)").
				Value(&ticketID).
				Validate(func(str string) error {
					if str == "" {
						return fmt.Errorf("el identificador del ticket no puede estar vacío")
					}
					return nil
				}),

			huh.NewInput().
				Title("Duración (minutos)").
				Description("Tiempo dedicado a esta sesión").
				Value(&duration).
				Validate(func(str string) error {
					mins, err := strconv.Atoi(str)
					if err != nil || mins <= 0 {
						return fmt.Errorf("debe ser un número entero positivo")
					}
					return nil
				}),

			huh.NewSelect[string]().
				Title("Bucket de Capacidad (Carga Alostática)").
				Description("Clasificación de energía y contexto").
				Options(
					huh.NewOption("Deep Work (Foco, Desarrollo, Arquitectura)", "deep_work"),
					huh.NewOption("Toil Support (Soporte ad-hoc, Incidencias)", "toil_support"),
					huh.NewOption("Coordination (Reuniones, Alineación)", "coordination"),
					huh.NewOption("Standby (Bloqueos, Espera externa)", "standby"),
				).
				Value(&bucket),

			huh.NewInput().
				Title("Notas / Contexto").
				Description("Breve descripción del avance o causa de fricción").
				Value(&notes),
		),
	).WithTheme(huh.ThemeCharm())

	if err := form.Run(); err != nil {
		return err
	}

	mins, _ := strconv.Atoi(duration)
	return svc.LogTime(ctx, ticketID, mins, ledger.CapacityBucket(bucket), notes)
}

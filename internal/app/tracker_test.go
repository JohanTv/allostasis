package app_test

import (
	"context"
	"testing"

	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/app"
	"github.com/allostasis/alo/internal/domain/issue"
)

func TestTrackerService_CRUD_LocalFirst(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error abriendo store: %v", err)
	}
	defer store.Close()

	svc := app.NewTrackerService(store, nil)

	// 1. Crear tarea local
	created, err := svc.Create(ctx, issue.CreateIssueInput{
		ProjectKey:  "OMNIFY",
		Title:       "Implementar módulo de búsqueda en Go",
		Description: "Soporte FTS para títulos y descripciones de tickets",
		Priority:    "high",
		StateName:   "Pendiente",
	})
	if err != nil {
		t.Fatalf("Create falló: %v", err)
	}
	if created.Title != "Implementar módulo de búsqueda en Go" {
		t.Errorf("título inesperado: %s", created.Title)
	}

	// 2. Obtener por Key
	item, err := svc.Get(ctx, created.Key)
	if err != nil {
		t.Fatalf("Get falló: %v", err)
	}
	if item.Priority != "high" {
		t.Errorf("prioridad inesperada: %s", item.Priority)
	}

	// 3. Buscar por texto
	results, err := svc.Search(ctx, "módulo de búsqueda", "OMNIFY", 5)
	if err != nil {
		t.Fatalf("Search falló: %v", err)
	}
	if len(results) != 1 || results[0].Key != created.Key {
		t.Fatalf("se esperaba encontrar la tarea creada por búsqueda, obtuvo: %+v", results)
	}

	// 4. Actualizar estado
	newTitle := "Módulo de búsqueda en Go completado"
	newState := "Progreso"
	updated, err := svc.Update(ctx, created.Key, issue.UpdateIssueInput{
		Title:     &newTitle,
		StateName: &newState,
	})
	if err != nil {
		t.Fatalf("Update falló: %v", err)
	}
	if updated.Title != newTitle || updated.StateName != newState {
		t.Errorf("actualización no reflejada: %+v", updated)
	}
}

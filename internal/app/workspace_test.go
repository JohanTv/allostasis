package app_test

import (
	"context"
	"testing"

	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/app"
	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/domain/workspace"
)

type mockWorkspaceRunner struct {
	createdContainer *workspace.Container
	lastStatus       func(ticketID string) (string, string)
}

func (m *mockWorkspaceRunner) ResolveRepo(ctx context.Context, token string) (string, error) {
	return "/abs/path/" + token, nil
}

func (m *mockWorkspaceRunner) CreateWorktree(ctx context.Context, c *workspace.Container) error {
	m.createdContainer = c
	return nil
}

func (m *mockWorkspaceRunner) ListContainers(ctx context.Context, baseDir string) ([]workspace.Container, error) {
	return []workspace.Container{
		{TicketID: "ALO-10", Slug: "alo-10", Path: baseDir + "/alo-10"},
	}, nil
}

func (m *mockWorkspaceRunner) AuditContainers(ctx context.Context, baseDir, target, preferredBase string, noFetch bool, statusProvider app.StatusProvider) ([]workspace.ContainerAudit, error) {
	m.lastStatus = statusProvider
	group, name := statusProvider("ALO-10")
	return []workspace.ContainerAudit{
		{Container: "alo-10", Category: workspace.CategorySeguroParaPodar, Reason: "Group: " + group + ", Name: " + name},
	}, nil
}

func (m *mockWorkspaceRunner) PruneContainers(ctx context.Context, baseDir, target, preferredBase string, execute, includeStale, noFetch bool, backupDir string, statusProvider app.StatusProvider) (*workspace.PruneResult, error) {
	m.lastStatus = statusProvider
	return &workspace.PruneResult{
		PrunedSafe: []string{"alo-10"},
	}, nil
}

func TestWorkspaceService_Create_List_Audit(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error creando store en memoria: %v", err)
	}
	defer store.Close()

	// Guardar estado del ticket ALO-10
	_ = store.SaveIssue(ctx, &issue.WorkItem{
		Key:        "ALO-10",
		Title:      "Refactor arquitectura",
		StateGroup: "done",
		StateName:  "Completado",
	})

	runner := &mockWorkspaceRunner{}
	svc := app.NewWorkspaceService(runner, store)

	// 1. Create
	container, err := svc.Create(ctx, app.CreateContainerInput{
		TicketID:   "ALO-10",
		Slug:       "alo-10-refactor",
		RepoTokens: []string{"repoA", "repoB"},
		BranchType: "feat",
		BaseRef:    "main",
	})
	if err != nil {
		t.Fatalf("Create falló: %v", err)
	}
	if container.Slug != "alo-10-refactor" || len(container.Bindings) != 2 {
		t.Errorf("contenedor creado inválido: %+v", container)
	}

	// 2. List
	containers, err := svc.List(ctx, "")
	if err != nil {
		t.Fatalf("List falló: %v", err)
	}
	if len(containers) != 1 || containers[0].TicketID != "ALO-10" {
		t.Errorf("listado inesperado: %+v", containers)
	}

	// 3. Audit con statusProvider integrado
	audits, err := svc.Audit(ctx, "", "", "main", false)
	if err != nil {
		t.Fatalf("Audit falló: %v", err)
	}
	if len(audits) != 1 || audits[0].Reason != "Group: done, Name: Completado" {
		t.Errorf("resultado de auditoría inesperado: %+v", audits)
	}

	// 4. Prune
	pruneRes, err := svc.Prune(ctx, "", "", "main", true, false, false, "")
	if err != nil {
		t.Fatalf("Prune falló: %v", err)
	}
	if len(pruneRes.PrunedSafe) != 1 || pruneRes.PrunedSafe[0] != "alo-10" {
		t.Errorf("resultado de prune inesperado: %+v", pruneRes)
	}
}

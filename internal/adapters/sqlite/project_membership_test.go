package sqlite_test

import (
	"context"
	"testing"

	"github.com/JohanTv/allostasis/internal/adapters/sqlite"
	"github.com/JohanTv/allostasis/internal/domain/issue"
)

func boolPtr(b bool) *bool { return &b }

// La pertenencia se persiste y se lee: false explícito se conserva; true y desconocido (nil) cuentan como miembro.
func TestProjects_MembershipRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error creando store: %v", err)
	}
	defer store.Close()

	for _, p := range []issue.ProjectRecord{
		{Identifier: "OMNIFY", ProjectID: "p1", Name: "Omnify", IsMember: boolPtr(true)},
		{Identifier: "OMNILEADS", ProjectID: "p2", Name: "Omnileads", IsMember: boolPtr(false)},
		{Identifier: "OPS", ProjectID: "p3", Name: "Ops"}, // desconocido
	} {
		if err := store.SaveProject(ctx, p); err != nil {
			t.Fatalf("SaveProject %s: %v", p.Identifier, err)
		}
	}

	list, err := store.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	byID := map[string]issue.ProjectRecord{}
	for _, p := range list {
		byID[p.Identifier] = p
	}
	if byID["OMNIFY"].IsKnownNonMember() || byID["OPS"].IsKnownNonMember() {
		t.Errorf("OMNIFY y OPS (miembro/desconocido) no deben ser no-miembro: %+v %+v", byID["OMNIFY"], byID["OPS"])
	}
	if !byID["OMNILEADS"].IsKnownNonMember() {
		t.Errorf("OMNILEADS debe persistirse como no miembro: %+v", byID["OMNILEADS"])
	}

	got, err := store.GetProject(ctx, "OMNILEADS")
	if err != nil || !got.IsKnownNonMember() {
		t.Errorf("GetProject debe devolver la pertenencia: %+v err=%v", got, err)
	}

	// Si Plane lo vuelve a hacer miembro, el siguiente SaveProject lo corrige (upsert).
	if err := store.SaveProject(ctx, issue.ProjectRecord{Identifier: "OMNILEADS", ProjectID: "p2", Name: "Omnileads", IsMember: boolPtr(true)}); err != nil {
		t.Fatalf("SaveProject upsert: %v", err)
	}
	if got, _ := store.GetProject(ctx, "OMNILEADS"); got.IsKnownNonMember() {
		t.Errorf("tras volver a ser miembro ya no debe quedar excluido: %+v", got)
	}
}

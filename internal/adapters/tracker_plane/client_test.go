package tracker_plane_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/allostasis/alo/internal/adapters/tracker_plane"
	"github.com/allostasis/alo/internal/domain/issue"
)

func TestClient_SendCommentRaw(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "secret-token-123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/api/v1/workspaces/test-workspace/projects/proj-uuid-1/issues/issue-uuid-1/comments/" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"comment-plane-777"}`))
	}))
	defer server.Close()

	cfg := tracker_plane.Config{
		BaseURL:   server.URL,
		APIKey:    "secret-token-123",
		Workspace: "test-workspace",
		Timeout:   2 * time.Second,
	}
	client := tracker_plane.NewClient(cfg)
	ctx := context.Background()

	commentID, err := client.SendCommentRaw(ctx, "test-workspace", "proj-uuid-1", "issue-uuid-1", `{"comment_html":"<p>test</p>"}`)
	if err != nil {
		t.Fatalf("SendCommentRaw falló: %v", err)
	}
	if commentID != "comment-plane-777" {
		t.Errorf("commentID inesperado: %s", commentID)
	}
}

func TestClient_TrackerOperations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "secret-token-123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/users/me/":
			_, _ = w.Write([]byte(`{"id":"usr-1","email":"johan@example.com","first_name":"Johan","last_name":"Tv","display_name":"johantv"}`))

		case r.URL.Path == "/api/v1/workspaces/test-workspace/projects/":
			_, _ = w.Write([]byte(`{"results":[{"id":"proj-omnify","identifier":"OMNIFY","name":"Omnify Platform"}]}`))

		case r.URL.Path == "/api/v1/workspaces/test-workspace/projects/proj-omnify/states/":
			_, _ = w.Write([]byte(`{"results":[{"id":"st-prog","name":"Progreso","slug":"progreso","group":"started"}]}`))

		case r.URL.Path == "/api/v1/workspaces/test-workspace/projects/proj-omnify/issues/" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"results":[
				{"id":"iss-1","sequence_id":269,"name":"[FEAT] Canal de atención Facebook Messenger e Instagram DM","priority":"high","state_detail":{"name":"Backlog","group":"backlog"},"labels":["backend","feat"]},
				{"id":"iss-2","sequence_id":270,"name":"[FEAT] Segundo canal","priority":"medium","state":"st-prog","assignees":["usr-1"],"labels":["frontend"]}
			]}`))

		case r.URL.Path == "/api/v1/workspaces/test-workspace/projects/proj-omnify/issues/" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"iss-created","sequence_id":270,"name":"[FEAT] Nuevo Canal","priority":"medium"}`))

		case r.URL.Path == "/api/v1/workspaces/test-workspace/projects/proj-omnify/issues/iss-1/" && r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"iss-1","name":"Actualizado"}`))

		case r.URL.Path == "/api/v1/workspaces/test-workspace/projects/proj-omnify/labels/":
			_, _ = w.Write([]byte(`{"results":[{"id":"lbl-1","name":"backend","color":"#ff0000"}]}`))

		case r.URL.Path == "/api/v1/workspaces/test-workspace/projects/proj-omnify/issues/iss-1/links/":
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":"lnk-1","title":"PR 10","url":"https://github.com/org/repo/pull/10"}`))
			} else {
				_, _ = w.Write([]byte(`{"results":[{"id":"lnk-1","title":"PR 10","url":"https://github.com/org/repo/pull/10"}]}`))
			}

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := tracker_plane.Config{
		BaseURL:   server.URL,
		APIKey:    "secret-token-123",
		Workspace: "test-workspace",
		Timeout:   2 * time.Second,
	}
	client := tracker_plane.NewClient(cfg)
	ctx := context.Background()

	// 1. Whoami
	user, err := client.Whoami(ctx)
	if err != nil {
		t.Fatalf("Whoami falló: %v", err)
	}
	if user.Email != "johan@example.com" || user.Username != "johantv" {
		t.Errorf("usuario inesperado: %+v", user)
	}

	// 2. FetchProjects
	projects, err := client.FetchProjects(ctx)
	if err != nil {
		t.Fatalf("FetchProjects falló: %v", err)
	}
	if len(projects) != 1 || projects[0].Identifier != "OMNIFY" {
		t.Errorf("proyectos inesperados: %+v", projects)
	}

	// 3. FetchStates
	states, err := client.FetchStates(ctx, "proj-omnify")
	if err != nil {
		t.Fatalf("FetchStates falló: %v", err)
	}
	if len(states) != 1 || states[0].Slug != "progreso" {
		t.Errorf("estados inesperados: %+v", states)
	}

	// 4. FetchIssues
	issues, err := client.FetchIssues(ctx, "proj-omnify", "OMNIFY", 50)
	if err != nil {
		t.Fatalf("FetchIssues falló: %v", err)
	}
	if len(issues) != 2 || issues[0].Item.Key != "OMNIFY-269" {
		t.Errorf("issues inesperados: %+v", issues)
	}
	if issues[1].Item.StateName != "Progreso" || issues[1].Item.StateGroup != "started" {
		t.Errorf("estado resuelto inesperado para iss-2: %s (%s)", issues[1].Item.StateName, issues[1].Item.StateGroup)
	}
	if len(issues[1].Item.Assignees) != 1 || issues[1].Item.Assignees[0] != "usr-1" {
		t.Errorf("assignees inesperados: %+v", issues[1].Item.Assignees)
	}

	// 5. CreateIssueRemote
	created, err := client.CreateIssueRemote(ctx, "proj-omnify", issue.CreateIssueInput{
		ProjectKey: "OMNIFY",
		Title:      "[FEAT] Nuevo Canal",
		Priority:   "medium",
	}, "st-prog")
	if err != nil {
		t.Fatalf("CreateIssueRemote falló: %v", err)
	}
	if created.Key != "OMNIFY-270" {
		t.Errorf("clave inesperada para tarea creada: %s", created.Key)
	}

	// 6. UpdateIssueRemote
	title := "Actualizado"
	err = client.UpdateIssueRemote(ctx, "proj-omnify", "iss-1", issue.UpdateIssueInput{
		Title: &title,
	}, "")
	if err != nil {
		t.Fatalf("UpdateIssueRemote falló: %v", err)
	}

	// 7. FetchLabels
	labels, err := client.FetchLabels(ctx, "proj-omnify")
	if err != nil {
		t.Fatalf("FetchLabels falló: %v", err)
	}
	if len(labels) != 1 || labels[0].Name != "backend" {
		t.Errorf("etiquetas inesperadas: %+v", labels)
	}

	// 8. AddLinkRemote & ListLinksRemote
	link, err := client.AddLinkRemote(ctx, "proj-omnify", "iss-1", "PR 10", "https://github.com/org/repo/pull/10")
	if err != nil {
		t.Fatalf("AddLinkRemote falló: %v", err)
	}
	if link.ID != "lnk-1" {
		t.Errorf("link ID inesperado: %s", link.ID)
	}
	links, err := client.ListLinksRemote(ctx, "proj-omnify", "iss-1")
	if err != nil {
		t.Fatalf("ListLinksRemote falló: %v", err)
	}
	if len(links) != 1 || links[0].Title != "PR 10" {
		t.Errorf("enlaces inesperados: %+v", links)
	}
}

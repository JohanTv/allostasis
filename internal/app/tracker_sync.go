package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/JohanTv/allostasis/internal/domain/issue"
)

// SyncProjects descarga el catálogo de proyectos y los almacena localmente.
func (s *TrackerService) SyncProjects(ctx context.Context) ([]issue.ProjectRecord, error) {
	if s.client == nil {
		return nil, fmt.Errorf("cliente de tracker remoto no configurado")
	}
	projects, err := s.client.FetchProjects(ctx)
	if err != nil {
		return nil, err
	}
	if s.store != nil {
		for _, p := range projects {
			_ = s.store.SaveProject(ctx, p)
		}
	}
	return projects, nil
}

// memberProjects descarta los proyectos de los que Plane informó que el usuario NO es miembro (públicos del
// workspace que solo se ven en la lista): consultarlos da 403 y desperdicia 3 peticiones por proyecto en cada sync.
// Un proyecto con pertenencia desconocida (nil) se conserva.
func memberProjects(projects []issue.ProjectRecord) []issue.ProjectRecord {
	out := make([]issue.ProjectRecord, 0, len(projects))
	for _, p := range projects {
		if !p.IsKnownNonMember() {
			out = append(out, p)
		}
	}
	return out
}

// SyncStates descarga los estados de un proyecto y los almacena localmente.
func (s *TrackerService) SyncStates(ctx context.Context, projectID string) ([]issue.StateRecord, error) {
	if s.client == nil {
		return nil, fmt.Errorf("cliente de tracker remoto no configurado")
	}
	states, err := s.client.FetchStates(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if s.store != nil {
		for _, st := range states {
			_ = s.store.SaveState(ctx, st)
		}
	}
	return states, nil
}

// SyncLabels descarga las etiquetas de un proyecto y las almacena localmente.
func (s *TrackerService) SyncLabels(ctx context.Context, projectID string) (int, error) {
	if s.client == nil {
		return 0, fmt.Errorf("cliente de tracker remoto no configurado")
	}
	labels, err := s.client.FetchLabels(ctx, projectID)
	if err != nil {
		return 0, err
	}
	count := 0
	if s.store != nil {
		for _, l := range labels {
			if err := s.store.SaveLabel(ctx, l); err == nil {
				count++
			}
		}
	}
	return count, nil
}

// SyncCatalogs sincroniza proyectos, estados y etiquetas de un proyecto específico (o por defecto).
func (s *TrackerService) SyncCatalogs(ctx context.Context, projectKey string) error {
	if s.client == nil {
		return fmt.Errorf("cliente de tracker remoto no configurado")
	}
	projects, err := s.SyncProjects(ctx)
	if err != nil {
		return fmt.Errorf("error sincronizando proyectos: %w", err)
	}

	targetProjectID := ""
	if projectKey != "" {
		for _, p := range projects {
			if strings.EqualFold(p.Identifier, projectKey) {
				targetProjectID = p.ProjectID
				break
			}
		}
	} else if len(projects) > 0 {
		targetProjectID = projects[0].ProjectID
	}

	if targetProjectID != "" {
		_, _ = s.SyncStates(ctx, targetProjectID)
		_, _ = s.SyncLabels(ctx, targetProjectID)
	}
	return nil
}

// SyncIssuesDelta descarga las tareas de un proyecto aplicando soporte de parada temprana por marca de agua (cutoffISO).
func (s *TrackerService) SyncIssuesDelta(ctx context.Context, projectID, projectKey string, limit int, cutoffISO string, deep bool) (int, string, int, error) {
	if s.client == nil {
		return 0, "", 0, fmt.Errorf("cliente de tracker remoto no configurado")
	}

	var (
		states    []issue.StateRecord
		snapshots []issue.IssueSnapshot
		newestUp  string
		fetchErr  error
		wg        sync.WaitGroup
	)

	wg.Add(3)
	go func() {
		defer wg.Done()
		states, _ = s.SyncStates(ctx, projectID)
	}()
	go func() {
		defer wg.Done()
		_, _ = s.SyncLabels(ctx, projectID)
	}()
	go func() {
		defer wg.Done()
		snapshots, newestUp, fetchErr = s.client.FetchIssuesDelta(ctx, projectID, projectKey, limit, cutoffISO, deep)
	}()
	wg.Wait()

	if fetchErr != nil {
		return 0, newestUp, 0, fetchErr
	}

	stateMap := make(map[string]issue.StateRecord, len(states))
	for _, st := range states {
		stateMap[st.ID] = st
	}

	syncedCount := 0
	if s.store != nil {
		for _, snap := range snapshots {
			if (snap.Item.StateName == "" || snap.Item.StateName == "Backlog") && snap.RawJSON != "" {
				var raw map[string]interface{}
				if err := json.Unmarshal([]byte(snap.RawJSON), &raw); err == nil {
					if sID, ok := raw["state"].(string); ok && sID != "" {
						if st, found := stateMap[sID]; found {
							snap.Item.StateName = st.Name
							snap.Item.StateGroup = st.GroupName
							snap.Item.State = issue.State(st.GroupName)
						}
					}
				}
			}
			if err := s.store.SaveIssue(ctx, &snap.Item); err == nil {
				_ = s.store.SaveIssueSnapshot(ctx, snap.Item.ID, snap.Item.Key, snap.RawJSON)
				syncedCount++
			}
		}
	}
	pagesFetched := 1
	if len(snapshots) > 100 {
		pagesFetched = (len(snapshots) + 99) / 100
	}
	return syncedCount, newestUp, pagesFetched, nil
}

// SyncIssues descarga las tareas de un proyecto y las sincroniza en caché y snapshots.
func (s *TrackerService) SyncIssues(ctx context.Context, projectKey string, limit int) (int, error) {
	if s.client == nil {
		return 0, fmt.Errorf("cliente de tracker remoto no configurado")
	}
	projectKey = strings.ToUpper(strings.TrimSpace(projectKey))
	var projectID string
	if s.store != nil {
		if pRec, err := s.store.GetProject(ctx, projectKey); err == nil && pRec != nil && pRec.ProjectID != "" {
			projectID = pRec.ProjectID
		}
	}
	if projectID == "" {
		projects, err := s.SyncProjects(ctx)
		if err != nil {
			return 0, fmt.Errorf("no se pudo resolver proyecto %s: %w", projectKey, err)
		}
		for _, p := range projects {
			if p.Identifier == projectKey {
				projectID = p.ProjectID
				break
			}
		}
	}
	if projectID == "" {
		return 0, fmt.Errorf("proyecto %s no encontrado en Plane", projectKey)
	}

	cutoffISO := ""
	if s.store != nil {
		if wm, err := s.store.GetWatermark(ctx, "issues", projectID); err == nil && wm != nil {
			cutoffISO = wm.ServerHighWatermark
		}
	}

	projStart := s.now()
	count, newest, pages, err := s.SyncIssuesDelta(ctx, projectID, projectKey, limit, cutoffISO, false)
	if err == nil && s.store != nil {
		highWM := newest
		if highWM == "" {
			highWM = cutoffISO
		}
		_ = s.store.SetWatermark(ctx, issue.SyncWatermark{
			Domain:              "issues",
			ProjectID:           projectID,
			LastSyncTimestamp:   float64(s.now().Unix()),
			ServerHighWatermark: highWM,
			Status:              "synced",
			ItemsSynced:         count,
			PagesFetched:        pages,
			DurationMs:          time.Since(projStart).Milliseconds(),
			UpdatedAt:           s.now(),
		})
	}
	return count, err
}

// Sync sincroniza proyectos, estados y tareas desde Plane CE hacia la base de datos local.
func (s *TrackerService) Sync(ctx context.Context, projectKey string, allProjects bool) (int, error) {
	projects, err := s.SyncProjects(ctx)
	if err != nil {
		return 0, fmt.Errorf("error sincronizando proyectos: %w", err)
	}

	totalSynced := 0
	if allProjects {
		if len(projects) == 0 {
			return 0, nil
		}

		const maxWorkers = 5
		workers := maxWorkers
		if len(projects) < workers {
			workers = len(projects)
		}

		type syncResult struct {
			count int
			err   error
		}

		jobs := make(chan issue.ProjectRecord, len(projects))
		results := make(chan syncResult, len(projects))
		var wg sync.WaitGroup

		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for p := range jobs {
					if ctx.Err() != nil {
						results <- syncResult{err: ctx.Err()}
						continue
					}
					count, syncErr := s.SyncIssues(ctx, p.Identifier, 100)
					results <- syncResult{count: count, err: syncErr}
				}
			}()
		}

		for _, p := range projects {
			jobs <- p
		}
		close(jobs)

		wg.Wait()
		close(results)

		for res := range results {
			if res.err == nil {
				totalSynced += res.count
			}
		}

		if ctx.Err() != nil {
			return totalSynced, ctx.Err()
		}
	} else {
		if projectKey == "" {
			projectKey = "OMNIFY"
		}
		count, err := s.SyncIssues(ctx, projectKey, 100)
		if err != nil {
			return 0, err
		}
		totalSynced = count
	}

	return totalSynced, nil
}

// SyncUnified orquesta la sincronización soberana y unificada según el target solicitado (ADR 0028 / RFC 0027).
func (s *TrackerService) SyncUnified(ctx context.Context, opts issue.SyncUnifiedOptions) (*issue.SyncUnifiedReport, error) {
	startTime := s.now()
	target := strings.ToLower(strings.TrimSpace(opts.Target))
	if target == "" {
		target = "all"
	}

	report := &issue.SyncUnifiedReport{
		Status:     "synced",
		Target:     target,
		Watermarks: make(map[string]string),
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}

	// 1. Fase PUSH: Drenaje de Outbox (para target "all" o "outbox")
	if target == "all" || target == "outbox" {
		outboxRep, err := s.FlushOutbox(ctx, limit)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("flush outbox: %v", err))
		}
		if outboxRep != nil {
			report.SentComments += outboxRep.SentComments
			report.FailedComments += outboxRep.FailedComments
			report.Errors = append(report.Errors, outboxRep.Errors...)
		}
		if target == "outbox" {
			report.DurationMs = time.Since(startTime).Milliseconds()
			if len(report.Errors) > 0 {
				report.Status = "partial_success"
			}
			return report, nil
		}
	}

	// 2. Fase CATÁLOGOS: Proyectos, Estados y Etiquetas (para target "all" o "catalogs")
	var projects []issue.ProjectRecord
	if target == "all" || target == "catalogs" {
		projs, err := s.SyncProjects(ctx)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("sync projects: %v", err))
		} else {
			projects = projs
			report.SyncedProjects = len(projs)
			report.SyncedCatalogs = true
			// Sincronizar estados y etiquetas de cada proyecto del que el usuario es miembro
			for _, p := range memberProjects(projs) {
				_, _ = s.SyncStates(ctx, p.ProjectID)
				_, _ = s.SyncLabels(ctx, p.ProjectID)
			}
		}
		if target == "catalogs" {
			report.DurationMs = time.Since(startTime).Milliseconds()
			report.Status = "catalogs_synced"
			if len(report.Errors) > 0 {
				report.Status = "partial_success"
			}
			return report, nil
		}
	}

	// 3. Fase TICKETS: Delta Pull con Marcas de Agua (para target "all", "tickets", "issues")
	if target == "all" || target == "tickets" || target == "issues" {
		var targetProjects []issue.ProjectRecord
		if opts.ProjectKey != "" {
			var requestedKeys []string
			for _, k := range strings.Split(opts.ProjectKey, ",") {
				if t := strings.ToUpper(strings.TrimSpace(k)); t != "" {
					requestedKeys = append(requestedKeys, t)
				}
			}
			for _, targetKey := range requestedKeys {
				var pID string
				if s.store != nil {
					if pRec, err := s.store.GetProject(ctx, targetKey); err == nil && pRec != nil && pRec.ProjectID != "" {
						pID = pRec.ProjectID
					}
				}
				if pID == "" {
					if len(projects) == 0 {
						projects, _ = s.SyncProjects(ctx)
					}
					for _, p := range projects {
						if p.Identifier == targetKey {
							pID = p.ProjectID
							break
						}
					}
				}
				if pID != "" {
					targetProjects = append(targetProjects, issue.ProjectRecord{Identifier: targetKey, ProjectID: pID})
				} else {
					report.Errors = append(report.Errors, fmt.Sprintf("proyecto %s no encontrado", targetKey))
				}
			}
		} else if opts.AllProjects {
			if len(projects) == 0 {
				projects, _ = s.SyncProjects(ctx)
			}
			targetProjects = projects
		} else {
			// Por defecto: los proyectos de los que el usuario es miembro (los públicos donde no lo es se omiten)
			if len(projects) == 0 {
				projects, _ = s.SyncProjects(ctx)
			}
			targetProjects = memberProjects(projects)
		}

		// Un 403 sobre UN proyecto concreto (selección implícita) significa "no tengo permiso sobre ese proyecto":
		// se omite y se informa aparte (SkippedProjects), no como error. Si lo piden explícito (-p) o si TODOS los
		// proyectos dan 403 (credenciales o permisos mal configurados), sigue siendo error. El 401 nunca se omite.
		var forbiddenErrs []string
		for _, p := range targetProjects {
			cutoffISO := ""
			if !opts.Deep && s.store != nil {
				if wm, err := s.store.GetWatermark(ctx, "issues", p.ProjectID); err == nil && wm != nil {
					cutoffISO = wm.ServerHighWatermark
				}
			}

			projStart := s.now()
			synced, newestUp, pages, err := s.SyncIssuesDelta(ctx, p.ProjectID, p.Identifier, limit, cutoffISO, opts.Deep)
			if err != nil {
				var authErr *issue.AuthError
				if opts.ProjectKey == "" && errors.As(err, &authErr) && authErr.Status == 403 {
					report.SkippedProjects = append(report.SkippedProjects, issue.SkippedProject{Project: p.Identifier, Reason: "sin permiso (HTTP 403)"})
					forbiddenErrs = append(forbiddenErrs, fmt.Sprintf("sync issues %s: %v", p.Identifier, err))
					continue
				}
				report.Errors = append(report.Errors, fmt.Sprintf("sync issues %s: %v", p.Identifier, err))
				continue
			}

			report.SyncedIssues += synced
			highWM := newestUp
			if highWM == "" {
				highWM = cutoffISO
			}
			report.Watermarks[p.Identifier] = highWM

			if s.store != nil {
				_ = s.store.SetWatermark(ctx, issue.SyncWatermark{
					Domain:              "issues",
					ProjectID:           p.ProjectID,
					LastSyncTimestamp:   float64(s.now().Unix()),
					ServerHighWatermark: highWM,
					Status:              "synced",
					ItemsSynced:         synced,
					PagesFetched:        pages,
					DurationMs:          time.Since(projStart).Milliseconds(),
					UpdatedAt:           s.now(),
				})
			}
		}
		if len(forbiddenErrs) > 0 && len(forbiddenErrs) == len(targetProjects) {
			report.Errors = append(report.Errors, forbiddenErrs...)
			report.SkippedProjects = nil
		}
	}

	// 4. Fase HOURS / WORKLOGS
	if target == "hours" || target == "worklogs" {
		outboxRep, err := s.FlushOutbox(ctx, limit)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("flush worklogs: %v", err))
		}
		if outboxRep != nil {
			report.SentComments += outboxRep.SentComments
			report.FailedComments += outboxRep.FailedComments
			report.Errors = append(report.Errors, outboxRep.Errors...)
		}
	}

	report.DurationMs = time.Since(startTime).Milliseconds()
	if len(report.Errors) > 0 {
		if report.SyncedIssues > 0 || report.SentComments > 0 || report.SyncedCatalogs {
			report.Status = "partial_success"
		} else {
			report.Status = "failed"
		}
	}

	return report, nil
}

// Whoami consulta la identidad y validez de la sesión en Plane CE y la persiste localmente.
func (s *TrackerService) Whoami(ctx context.Context) (*issue.UserInfo, error) {
	if s.client == nil {
		return nil, fmt.Errorf("cliente de Plane no configurado")
	}
	user, err := s.client.Whoami(ctx)
	if err == nil && user != nil && user.ID != "" && s.store != nil {
		_ = s.store.SaveUserProfile(ctx, *user)
	}
	return user, err
}

// ResolveCurrentUserID determina el UUID del usuario actual mediante la jerarquía canónica Fail-Closed:
// 1. PLANE_ACTOR_UUID (variable de entorno, 0ms)
// 2. user_profile en SQLite local (<1ms, 100% offline)
// 3. HTTP Whoami hacia el tracker remoto (guarda en SQLite si tiene éxito)
// 4. Retorna error si no se pudo determinar la identidad (Fail-Closed).
func (s *TrackerService) ResolveCurrentUserID(ctx context.Context) (string, error) {
	// 1. Variable de entorno explícita
	if actorUUID := strings.TrimSpace(os.Getenv("PLANE_ACTOR_UUID")); actorUUID != "" {
		return actorUUID, nil
	}

	// 2. Caché local en SQLite
	if s.store != nil {
		if u, err := s.store.GetUserProfile(ctx); err == nil && u != nil && strings.TrimSpace(u.ID) != "" {
			return strings.TrimSpace(u.ID), nil
		}
	}

	// 3. Fallback a Whoami remoto (y auto-caché)
	if s.client != nil {
		if u, err := s.Whoami(ctx); err == nil && u != nil && strings.TrimSpace(u.ID) != "" {
			return strings.TrimSpace(u.ID), nil
		}
	}

	// 4. Fail-Closed
	return "", fmt.Errorf("no se pudo determinar tu identidad de usuario; configura PLANE_ACTOR_UUID o ejecuta 'alo ticket whoami' con conexión")
}

// Projects retorna los proyectos registrados en la caché local.
func (s *TrackerService) Projects(ctx context.Context) ([]issue.ProjectRecord, error) {
	return s.store.ListProjects(ctx)
}

// States retorna los estados locales, sincronizándolos con Plane si se solicita refresh o si la caché está vacía.
func (s *TrackerService) States(ctx context.Context, projectKey string, refresh bool) ([]issue.StateRecord, error) {
	if s.store == nil {
		return nil, fmt.Errorf("almacén local no inicializado")
	}

	var projectID string
	if projectKey != "" {
		if p, err := s.store.GetProject(ctx, strings.ToUpper(projectKey)); err == nil && p != nil {
			projectID = p.ProjectID
		} else {
			projectID = projectKey
		}
	}

	states, err := s.store.ListStates(ctx, projectKey)
	if err != nil {
		return nil, err
	}

	if (refresh || len(states) == 0) && s.client != nil {
		if projectID != "" {
			if _, err := s.SyncStates(ctx, projectID); err == nil {
				states, _ = s.store.ListStates(ctx, projectKey)
			}
		} else {
			if projs, err := s.Projects(ctx); err == nil {
				for _, p := range projs {
					_, _ = s.SyncStates(ctx, p.ProjectID)
				}
				states, _ = s.store.ListStates(ctx, "")
			}
		}
	}

	return states, nil
}

// Labels retorna las etiquetas locales, sincronizándolas con Plane si se solicita refresh.
func (s *TrackerService) Labels(ctx context.Context, projectKey string, refresh bool) ([]issue.Label, error) {
	var projectID string
	if projectKey != "" {
		if p, err := s.store.GetProject(ctx, strings.ToUpper(projectKey)); err == nil && p != nil {
			projectID = p.ProjectID
		}
	} else {
		if p, err := s.store.GetProject(ctx, "OMNIFY"); err == nil && p != nil {
			projectID = p.ProjectID
		}
	}

	labels, err := s.store.ListLabels(ctx, projectID)
	if err != nil {
		return nil, err
	}

	if (refresh || len(labels) == 0) && s.client != nil && projectID != "" {
		if _, err := s.SyncLabels(ctx, projectID); err == nil {
			labels, _ = s.store.ListLabels(ctx, projectID)
		}
	}

	return labels, nil
}

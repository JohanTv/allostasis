package sqlite_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/domain/issue"
	"github.com/allostasis/alo/internal/domain/ledger"
)

// TestSQLite_Concurrency_FileBased prueba el comportamiento de SQLite con WAL y busy_timeout(10000)
// bajo alta concurrencia de lecturas y escrituras simultáneas en archivo real en disco.
func TestSQLite_Concurrency_FileBased(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "allostasis_stress.db")

	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("error creando store en archivo: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	const numWriters = 20
	const writesPerWorker = 20
	const numReaders = 10

	var wg sync.WaitGroup
	var busyErrors int64
	var totalWrites int64

	// Goroutines escritoras concurrentes
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < writesPerWorker; j++ {
				ticketKey := fmt.Sprintf("OMNIFY-%d-%d", workerID, j)
				item := &issue.WorkItem{
					ID:        fmt.Sprintf("id-%d-%d", workerID, j),
					Key:       ticketKey,
					Title:     fmt.Sprintf("Concurrent task %d-%d", workerID, j),
					State:     issue.StateStarted,
					Priority:  "high",
					Assignee:  fmt.Sprintf("worker-%d", workerID),
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}

				if err := store.SaveIssue(ctx, item); err != nil {
					atomic.AddInt64(&busyErrors, 1)
					t.Logf("Error en SaveIssue (worker %d): %v", workerID, err)
				} else {
					atomic.AddInt64(&totalWrites, 1)
				}

				// Insertar también worklog concurrentemente
				tNow := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC).Add(time.Duration(j) * time.Minute)
				entry := &ledger.WorklogEntry{
					IssueID:  ticketKey,
					Interval: ledger.Interval{Start: tNow, End: tNow.Add(15 * time.Minute)},
					Bucket:   ledger.BucketDeepWork,
					Topic:    "Concurrencia extrema",
				}
				if err := store.SaveWorklog(ctx, entry); err != nil {
					atomic.AddInt64(&busyErrors, 1)
					t.Logf("Error en SaveWorklog (worker %d): %v", workerID, err)
				}
			}
		}(i)
	}

	// Goroutines lectoras concurrentes (mientras se escribe)
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for j := 0; j < 15; j++ {
				_, _ = store.ListIssues(ctx, "")
				_, _ = store.GetIntervals(ctx)
				time.Sleep(5 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Escrituras exitosas en archivo: %d | Errores de bloqueo/busy: %d", totalWrites, busyErrors)
	if busyErrors > 0 {
		t.Errorf("FALLO CONCURRENCIA WAL: Se presentaron %d errores SQLITE_BUSY / bloqueo en archivo con busy_timeout configurado", busyErrors)
	}
}

// TestSQLite_Concurrency_InMemory evalúa el comportamiento del store configurado con ':memory:'
// donde el DSN file::memory:?mode=memory&cache=shared provoca que cada conexión del pool
// cree una base de datos privada en memoria aislada, perdiendo el esquema migrado.
func TestSQLite_Concurrency_InMemory(t *testing.T) {
	store, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("error creando store en memoria: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	const numWriters = 5
	const writesPerWorker = 5

	var wg sync.WaitGroup
	var missingTableErrors int64
	var successWrites int64

	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < writesPerWorker; j++ {
				item := &issue.WorkItem{
					ID:       fmt.Sprintf("mem-id-%d-%d", workerID, j),
					Key:      fmt.Sprintf("MEM-%d-%d", workerID, j),
					Title:    "Tarea en memoria",
					State:    issue.StateBacklog,
					Priority: "medium",
				}

				if err := store.SaveIssue(ctx, item); err != nil {
					if strings.Contains(err.Error(), "no such table") {
						atomic.AddInt64(&missingTableErrors, 1)
					}
					t.Logf("Error en memoria (worker %d): %v", workerID, err)
				} else {
					atomic.AddInt64(&successWrites, 1)
				}
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Escrituras exitosas en memoria: %d | Errores por tabla inexistente (conexiones aisladas): %d", successWrites, missingTableErrors)
	if missingTableErrors > 0 {
		t.Errorf("DEFECTO CRÍTICO DE ARQUITECTURA: ':memory:' crea bases anónimas privadas por cada conexión del pool (MaxOpenConns=10), provocando 'no such table' en goroutines concurrentes")
	}
}

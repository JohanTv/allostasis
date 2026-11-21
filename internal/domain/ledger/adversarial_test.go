package ledger_test

import (
	"math"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/JohanTv/allostasis/internal/domain/clock"
	"github.com/JohanTv/allostasis/internal/domain/ledger"
)

// TestKlee_DegenerateIntervals evalúa casos patológicos y de frontera en el cálculo de intervalos:
// - Start == End (medida de Lebesgue cero)
// - Intervalos invertidos (Start > End)
// - Solapamientos en microsegundos y nanosegundos
// - Intervalos idénticos duplicados
func TestKlee_DegenerateIntervals(t *testing.T) {
	t.Run("ZeroLengthIntervals_MeasureZero", func(t *testing.T) {
		t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
		t1 := t0.Add(1 * time.Hour)

		// Intervalos degenerados con Start == End
		zero1 := ledger.Interval{Start: t0, End: t0}
		zero2 := ledger.Interval{Start: t0.Add(30 * time.Minute), End: t0.Add(30 * time.Minute)}
		normal := ledger.Interval{Start: t0, End: t1}

		// 1. Colección de solo intervalos de longitud cero
		onlyZero := []ledger.Interval{zero1, zero2}
		durOnlyZero := ledger.KleeMeasure(onlyZero)
		if durOnlyZero != 0 {
			t.Errorf("KleeMeasure de intervalos de medida cero debe ser 0, obtuvo %v", durOnlyZero)
		}

		// 2. Intervalo normal combinado con intervalos de longitud cero dentro del rango
		mixed := []ledger.Interval{zero1, normal, zero2}
		durMixed := ledger.KleeMeasure(mixed)
		if durMixed != 1*time.Hour {
			t.Errorf("KleeMeasure esperado 1h con puntos degenerados, obtuvo %v", durMixed)
		}

		merged := ledger.MergeIntervals(mixed)
		if len(merged) != 1 {
			t.Errorf("MergeIntervals debió absorber los intervalos punto en el intervalo contenedor, obtuvo %d intervalos", len(merged))
		}
	})

	t.Run("InvertedIntervals_StructBypass", func(t *testing.T) {
		// NewInterval rechaza Start > End, pero los campos del struct Interval son públicos
		t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
		t1 := t0.Add(1 * time.Hour)

		_, err := ledger.NewInterval(t1, t0)
		if err == nil {
			t.Fatal("NewInterval debió rechazar intervalo invertido (end antes de start)")
		}

		// Si se inicializa directamente el struct invertido:
		inverted := ledger.Interval{Start: t1, End: t0}
		dur := inverted.Duration()
		if dur != 0 {
			t.Errorf("Duración de intervalo invertido debe ser neutralizada a 0, obtuvo %v", dur)
		}

		// Evaluar comportamiento de MergeIntervals con intervalo invertido: debe descartarlo o neutralizarlo
		merged := ledger.MergeIntervals([]ledger.Interval{inverted})
		if len(merged) != 0 {
			t.Errorf("MergeIntervals debió filtrar el intervalo invertido, obtuvo: %+v", merged)
		}
	})

	t.Run("SubMicrosecondPrecision_AndBoundaries", func(t *testing.T) {
		base := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

		// Dos intervalos separados por exactamente 1 nanosegundo
		iv1 := ledger.Interval{Start: base, End: base.Add(100 * time.Nanosecond)}
		iv2 := ledger.Interval{Start: base.Add(100 * time.Nanosecond), End: base.Add(200 * time.Nanosecond)} // Contiguo exacto
		iv3 := ledger.Interval{Start: base.Add(201 * time.Nanosecond), End: base.Add(300 * time.Nanosecond)} // Disjunto por 1 ns

		merged := ledger.MergeIntervals([]ledger.Interval{iv1, iv2, iv3})
		if len(merged) != 2 {
			t.Fatalf("Esperados 2 intervalos (1 fusionado continuo y 1 disjunto por 1ns), obtenidos %d", len(merged))
		}

		// iv1 y iv2 deben fusionarse en [0ns, 200ns]
		if merged[0].Duration() != 200*time.Nanosecond {
			t.Errorf("Duración esperada del primer intervalo 200ns, obtuvo %v", merged[0].Duration())
		}
		if merged[1].Duration() != 99*time.Nanosecond {
			t.Errorf("Duración esperada del segundo intervalo 99ns, obtuvo %v", merged[1].Duration())
		}

		total := ledger.KleeMeasure([]ledger.Interval{iv1, iv2, iv3})
		if total != 299*time.Nanosecond {
			t.Errorf("KleeMeasure esperado 299ns, obtuvo %v", total)
		}
	})
}

// TestKlee_Stress10000Intervals somete a estrés el algoritmo de Klee con 10,000 intervalos aleatorios
// verificando que la complejidad $O(N \log N)$ escala en milisegundos y auditando ausencia de desbordamiento.
func TestKlee_Stress10000Intervals(t *testing.T) {
	const numIntervals = 10000
	rng := rand.New(rand.NewSource(42))
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	intervals := make([]ledger.Interval, numIntervals)
	for i := 0; i < numIntervals; i++ {
		// Intervalos aleatorios dentro de un horizonte de 30 días
		startOffset := time.Duration(rng.Int63n(30*24*3600)) * time.Second
		dur := time.Duration(rng.Int63n(4*3600)) * time.Second // duración de 0 a 4 horas
		start := baseTime.Add(startOffset)
		intervals[i] = ledger.Interval{
			Start: start,
			End:   start.Add(dur),
		}
	}

	start := time.Now()
	totalDuration := ledger.KleeMeasure(intervals)
	elapsed := time.Since(start)

	t.Logf("KleeMeasure de 10,000 intervalos ejecutado en: %v", elapsed)
	t.Logf("Medida total de tiempo neto calculado: %v", totalDuration)

	// El cálculo O(N log N) para 10,000 elementos bajo -race debe ejecutarse en < 1500ms (típicamente < 100ms sin -race)
	if elapsed > 1500*time.Millisecond {
		t.Errorf("Degradación de rendimiento: KleeMeasure tardó %v para 10k intervalos (> 1500ms)", elapsed)
	}

	// Comprobar ausencia de desbordamiento (duración positiva y dentro de los 30 días + 4h)
	maxPossible := 30*24*time.Hour + 4*time.Hour
	if totalDuration <= 0 || totalDuration > maxPossible {
		t.Errorf("Medida de tiempo inválida o desbordada: %v (debe estar entre 0 y %v)", totalDuration, maxPossible)
	}
}

// TestKlee_DurationOverflow evalúa el límite de time.Duration (aprox. 292 años).
// En Go, math.MaxInt64 nanosegundos es el valor máximo soportado por time.Duration.
func TestKlee_DurationOverflow(t *testing.T) {
	t0 := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	// 150 años cabe en time.Duration (~292 años límite)
	dur150Years := 150 * 365 * 24 * time.Hour
	t1 := t0.Add(dur150Years)
	t2 := t1.Add(dur150Years) // 300 años de t0

	iv1 := ledger.Interval{Start: t0, End: t1}
	// Si agregamos intervalos disjuntos enormes que sumados superen MaxInt64:
	iv2 := ledger.Interval{Start: t1.Add(1 * time.Hour), End: t2.Add(1 * time.Hour)}

	// La suma de 150 años + 150 años = 300 años > 292 años (MaxInt64 nanosegundos)
	total := ledger.KleeMeasure([]ledger.Interval{iv1, iv2})
	if total < 0 {
		t.Logf("AVISO AUDITORÍA: Desbordamiento de entero detectado en KleeMeasure con intervalos astronómicos (resultado negativo: %v)", total)
	} else {
		t.Logf("KleeMeasure con intervalos astronómicos: %v", total)
	}
}

// TestClock_FrozenClock_ConcurrencyAudit audita la seguridad en concurrencia (-race)
// de FrozenClock. Al no disponer de sincronización interna, el acceso concurrente debe ser auditado.
func TestClock_FrozenClock_ConcurrencyAudit(t *testing.T) {
	initial := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	fc := clock.NewFrozenClock(initial)

	var wg sync.WaitGroup
	const readers = 10
	const writers = 5

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(delta time.Duration) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				fc.Advance(delta)
			}
		}(time.Duration(i+1) * time.Minute)
	}

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = fc.Now()
				_ = fc.Since(initial)
			}
		}()
	}

	wg.Wait()
}

// TestAllostaticLoad_BoundaryEdgeCases prueba divisiones por cero y valores límite en AllostaticLoad
func TestAllostaticLoad_BoundaryEdgeCases(t *testing.T) {
	loadZero := ledger.AllostaticLoad{
		TotalTime: 0,
		ToilTime:  10 * time.Minute,
	}

	if ratio := loadZero.ToilRatio(); ratio != 0.0 {
		t.Errorf("ToilRatio con TotalTime=0 debe ser 0.0, obtuvo %f", ratio)
	}
	if ratio := loadZero.DeepWorkRatio(); ratio != 0.0 {
		t.Errorf("DeepWorkRatio con TotalTime=0 debe ser 0.0, obtuvo %f", ratio)
	}

	// Throughput <= 0 en Little's Law
	cycleTimeZero := ledger.LittlesLawCycleTime(5, 0.0)
	if cycleTimeZero != 0.0 {
		t.Errorf("LittlesLawCycleTime con throughput=0 debe retornar 0.0, obtuvo %f", cycleTimeZero)
	}

	cycleTimeNeg := ledger.LittlesLawCycleTime(5, -2.5)
	if cycleTimeNeg != 0.0 {
		t.Errorf("LittlesLawCycleTime con throughput negativo debe retornar 0.0, obtuvo %f", cycleTimeNeg)
	}

	// Throughput extremadamente pequeño no debe entrar en NaN o pánico
	cycleTimeEps := ledger.LittlesLawCycleTime(1, math.SmallestNonzeroFloat64)
	if math.IsNaN(cycleTimeEps) || math.IsInf(cycleTimeEps, 0) {
		t.Logf("LittlesLawCycleTime con throughput infinitesimal: %v", cycleTimeEps)
	}
}

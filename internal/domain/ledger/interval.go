package ledger

import (
	"errors"
	"sort"
	"time"
)

// Interval representa un intervalo continuo de tiempo en el ledger del desarrollador.
type Interval struct {
	Start time.Time
	End   time.Time
}

// NewInterval construye un intervalo validando que el inicio sea anterior o igual al fin.
func NewInterval(start, end time.Time) (Interval, error) {
	if end.Before(start) {
		return Interval{}, errors.New("intervalo inválido: end es anterior a start")
	}
	return Interval{Start: start, End: end}, nil
}

// Duration calcula la duración continua del intervalo.
func (i Interval) Duration() time.Duration {
	return i.End.Sub(i.Start)
}

// Minutes devuelve la duración en minutos enteros.
func (i Interval) Minutes() int {
	return int(i.Duration().Minutes())
}

// Overlaps determina si dos intervalos se solapan en el tiempo.
func (i Interval) Overlaps(other Interval) bool {
	return i.Start.Before(other.End) && other.Start.Before(i.End)
}

// MergeIntervals fusiona una colección de intervalos aplicando el algoritmo de Klee (Medida de Lebesgue en 1D).
// Resuelve solapamientos eliminando doble conteo y garantizando exactitud matemática.
func MergeIntervals(intervals []Interval) []Interval {
	if len(intervals) <= 1 {
		return intervals
	}

	// 1. Copiar y ordenar por Start ascendente
	sorted := make([]Interval, len(intervals))
	copy(sorted, intervals)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Start.Equal(sorted[j].Start) {
			return sorted[i].End.Before(sorted[j].End)
		}
		return sorted[i].Start.Before(sorted[j].Start)
	})

	// 2. Fusionar intervalos contiguos o solapados
	var merged []Interval
	current := sorted[0]

	for _, next := range sorted[1:] {
		if next.Start.Before(current.End) || next.Start.Equal(current.End) {
			if next.End.After(current.End) {
				current.End = next.End
			}
		} else {
			merged = append(merged, current)
			current = next
		}
	}
	merged = append(merged, current)

	return merged
}

// KleeMeasure calcula la suma total neta de tiempo sin solapamientos para una colección de intervalos.
func KleeMeasure(intervals []Interval) time.Duration {
	merged := MergeIntervals(intervals)
	var total time.Duration
	for _, iv := range merged {
		total += iv.Duration()
	}
	return total
}

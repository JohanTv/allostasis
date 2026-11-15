package ledger

import (
	"errors"
	"math"
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

// Duration calcula la duración continua del intervalo (siempre >= 0).
func (i Interval) Duration() time.Duration {
	if i.End.Before(i.Start) {
		return 0
	}
	return i.End.Sub(i.Start)
}

// Minutes devuelve la duración en minutos enteros.
func (i Interval) Minutes() int {
	return int(i.Duration().Minutes())
}

// Overlaps determina si dos intervalos válidos se solapan en el tiempo.
func (i Interval) Overlaps(other Interval) bool {
	if i.End.Before(i.Start) || other.End.Before(other.Start) {
		return false
	}
	return i.Start.Before(other.End) && other.Start.Before(i.End)
}

// MergeIntervals fusiona una colección de intervalos aplicando el algoritmo de Klee (Medida de Lebesgue en 1D).
// Resuelve solapamientos eliminando doble conteo y filtra defensivamente cualquier intervalo invertido.
func MergeIntervals(intervals []Interval) []Interval {
	if len(intervals) == 0 {
		return nil
	}

	// 1. Filtrar intervalos invertidos y puntos disjuntos degenerados si no aportan medida
	valid := make([]Interval, 0, len(intervals))
	for _, iv := range intervals {
		if !iv.End.Before(iv.Start) {
			valid = append(valid, iv)
		}
	}

	if len(valid) <= 1 {
		return valid
	}

	// 2. Ordenar por Start ascendente
	sort.Slice(valid, func(i, j int) bool {
		if valid[i].Start.Equal(valid[j].Start) {
			return valid[i].End.Before(valid[j].End)
		}
		return valid[i].Start.Before(valid[j].Start)
	})

	// 3. Fusionar intervalos contiguos o solapados
	var merged []Interval
	current := valid[0]

	for _, next := range valid[1:] {
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
// Incorpora guarda contra desbordamiento de enteros (math.MaxInt64).
func KleeMeasure(intervals []Interval) time.Duration {
	merged := MergeIntervals(intervals)
	var total time.Duration
	maxDuration := time.Duration(math.MaxInt64)

	for _, iv := range merged {
		d := iv.Duration()
		if d <= 0 {
			continue
		}
		if maxDuration-d < total {
			return maxDuration // Saturar al máximo físico sin desbordamiento negativo
		}
		total += d
	}
	return total
}

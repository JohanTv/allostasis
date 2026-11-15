package ledger_test

import (
	"testing"
	"time"

	"github.com/allostasis/alo/internal/domain/ledger"
)

func parseTime(s string) time.Time {
	t, _ := time.Parse("15:04", s)
	return t
}

func TestMergeIntervals_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		input    []ledger.Interval
		expected []ledger.Interval
	}{
		{
			name:     "vacío",
			input:    []ledger.Interval{},
			expected: []ledger.Interval{},
		},
		{
			name: "un solo intervalo",
			input: []ledger.Interval{
				{Start: parseTime("09:00"), End: parseTime("10:00")},
			},
			expected: []ledger.Interval{
				{Start: parseTime("09:00"), End: parseTime("10:00")},
			},
		},
		{
			name: "sin solapamiento",
			input: []ledger.Interval{
				{Start: parseTime("09:00"), End: parseTime("10:00")},
				{Start: parseTime("10:30"), End: parseTime("11:30")},
			},
			expected: []ledger.Interval{
				{Start: parseTime("09:00"), End: parseTime("10:00")},
				{Start: parseTime("10:30"), End: parseTime("11:30")},
			},
		},
		{
			name: "solapamiento parcial",
			input: []ledger.Interval{
				{Start: parseTime("09:00"), End: parseTime("10:15")},
				{Start: parseTime("09:45"), End: parseTime("11:00")},
			},
			expected: []ledger.Interval{
				{Start: parseTime("09:00"), End: parseTime("11:00")},
			},
		},
		{
			name: "intervalo completamente contenido",
			input: []ledger.Interval{
				{Start: parseTime("09:00"), End: parseTime("12:00")},
				{Start: parseTime("10:00"), End: parseTime("11:00")},
			},
			expected: []ledger.Interval{
				{Start: parseTime("09:00"), End: parseTime("12:00")},
			},
		},
		{
			name: "intervalos desordenados con múltiples solapamientos",
			input: []ledger.Interval{
				{Start: parseTime("14:00"), End: parseTime("15:00")},
				{Start: parseTime("09:00"), End: parseTime("10:00")},
				{Start: parseTime("09:30"), End: parseTime("10:30")},
				{Start: parseTime("13:30"), End: parseTime("14:30")},
			},
			expected: []ledger.Interval{
				{Start: parseTime("09:00"), End: parseTime("10:30")},
				{Start: parseTime("13:30"), End: parseTime("15:00")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ledger.MergeIntervals(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("longitud esperada %d, obtuvo %d", len(tt.expected), len(got))
			}
			for i := range got {
				if !got[i].Start.Equal(tt.expected[i].Start) || !got[i].End.Equal(tt.expected[i].End) {
					t.Errorf("en índice %d: esperado [%v, %v], obtuvo [%v, %v]",
						i, tt.expected[i].Start.Format("15:04"), tt.expected[i].End.Format("15:04"),
						got[i].Start.Format("15:04"), got[i].End.Format("15:04"))
				}
			}
		})
	}
}

func TestKleeMeasure(t *testing.T) {
	intervals := []ledger.Interval{
		{Start: parseTime("09:00"), End: parseTime("10:00")}, // 60 min
		{Start: parseTime("09:30"), End: parseTime("10:30")}, // solapa -> 90 min total
		{Start: parseTime("11:00"), End: parseTime("12:00")}, // 60 min
	}
	total := ledger.KleeMeasure(intervals)
	expected := 150 * time.Minute
	if total != expected {
		t.Fatalf("KleeMeasure esperado %v, obtuvo %v", expected, total)
	}
}

func TestAllostaticLoad_Ratios(t *testing.T) {
	load := ledger.AllostaticLoad{
		TotalTime:    10 * time.Hour,
		DeepWorkTime: 6 * time.Hour,
		ToilTime:     3 * time.Hour,
		Coordination: 1 * time.Hour,
		WIPCount:     3,
	}

	if load.ToilRatio() != 0.3 {
		t.Errorf("ToilRatio esperado 0.3, obtuvo %f", load.ToilRatio())
	}
	if load.DeepWorkRatio() != 0.6 {
		t.Errorf("DeepWorkRatio esperado 0.6, obtuvo %f", load.DeepWorkRatio())
	}

	// Little's Law: 3 tareas WIP / 0.5 tareas por día = 6 días de tiempo de ciclo
	cycleTime := ledger.LittlesLawCycleTime(load.WIPCount, 0.5)
	if cycleTime != 6.0 {
		t.Errorf("LittlesLawCycleTime esperado 6.0, obtuvo %f", cycleTime)
	}
}

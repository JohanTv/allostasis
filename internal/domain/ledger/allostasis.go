package ledger

import (
	"time"
)

// CapacityBucket categoriza el tipo de energía y demanda cognitiva del trabajo.
type CapacityBucket string

const (
	BucketDeepWork     CapacityBucket = "deep_work"    // Trabajo de foco, desarrollo de features y refactoring
	BucketToilSupport  CapacityBucket = "toil_support" // Soporte ad-hoc, resolución rápida de incidentes, mantenimiento
	BucketCoordination CapacityBucket = "coordination" // Reuniones, sincronizaciones de equipo y alineación
	BucketStandby      CapacityBucket = "standby"      // Tiempo de espera por bloqueos, revisiones o despliegues
)

// WorklogEntry representa un registro atómico de esfuerzo auditado en el ledger soberano.
type WorklogEntry struct {
	ID        string
	IssueID   string
	Interval  Interval
	Bucket    CapacityBucket
	Topic     string
	Author    string
	CreatedAt time.Time
}

// AllostaticLoad resume las métricas de carga cognitiva y desgaste operativo acumulado.
type AllostaticLoad struct {
	TotalTime       time.Duration // Tiempo total medido neto por Klee
	DeepWorkTime    time.Duration // Tiempo de foco protegido
	ToilTime        time.Duration // Tiempo de soporte invisible (toil)
	Coordination    time.Duration // Tiempo de reuniones
	StandbyTime     time.Duration // Tiempo en espera por bloqueos
	ContextSwitches int           // Cantidad de alternancias de contexto en el día
	WIPCount        int           // Número de tareas en progreso simultáneo
}

// ToilRatio calcula la proporción de esfuerzo absorbido por soporte no planificado frente al total.
func (a AllostaticLoad) ToilRatio() float64 {
	if a.TotalTime <= 0 {
		return 0.0
	}
	return float64(a.ToilTime) / float64(a.TotalTime)
}

// DeepWorkRatio calcula el porcentaje de tiempo dedicado a desarrollo de alto valor.
func (a AllostaticLoad) DeepWorkRatio() float64 {
	if a.TotalTime <= 0 {
		return 0.0
	}
	return float64(a.DeepWorkTime) / float64(a.TotalTime)
}

// LittlesLawCycleTime proyecta el tiempo medio de ciclo esperado en días basándose en la Ley de Little:
// W = L / λ (Tiempo de Ciclo = WIP / Tasa de Entrega Throughput)
func LittlesLawCycleTime(wip int, throughputPerDay float64) float64 {
	if throughputPerDay <= 0 {
		return 0.0
	}
	return float64(wip) / throughputPerDay
}

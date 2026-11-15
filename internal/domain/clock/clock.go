package clock

import "time"

// Clock abstrae el acceso al tiempo del sistema para garantizar determinismo absoluto en pruebas y auditorías.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
}

// RealClock implementa Clock delegando al reloj del sistema operativo.
type RealClock struct{}

func NewRealClock() Clock {
	return &RealClock{}
}

func (r *RealClock) Now() time.Time {
	return time.Now()
}

func (r *RealClock) Since(t time.Time) time.Duration {
	return time.Since(t)
}

// FrozenClock permite fijar y avanzar el tiempo de forma determinista para pruebas reproducibles.
type FrozenClock struct {
	currentTime time.Time
}

func NewFrozenClock(initial time.Time) *FrozenClock {
	return &FrozenClock{currentTime: initial}
}

func (f *FrozenClock) Now() time.Time {
	return f.currentTime
}

func (f *FrozenClock) Since(t time.Time) time.Duration {
	return f.currentTime.Sub(t)
}

func (f *FrozenClock) Set(t time.Time) {
	f.currentTime = t
}

func (f *FrozenClock) Advance(d time.Duration) {
	f.currentTime = f.currentTime.Add(d)
}

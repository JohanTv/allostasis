package clock_test

import (
	"testing"
	"time"

	"github.com/allostasis/alo/internal/domain/clock"
)

func TestRealClock(t *testing.T) {
	c := clock.NewRealClock()
	t1 := c.Now()
	time.Sleep(2 * time.Millisecond)
	if c.Since(t1) < 2*time.Millisecond {
		t.Errorf("Since esperaba al menos 2ms de diferencia, obtuvo %v", c.Since(t1))
	}
}

func TestFrozenClock(t *testing.T) {
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	fc := clock.NewFrozenClock(base)

	if !fc.Now().Equal(base) {
		t.Fatalf("esperado %v, obtuvo %v", base, fc.Now())
	}

	fc.Advance(30 * time.Minute)
	expectedAdvanced := time.Date(2026, 11, 15, 12, 30, 0, 0, time.UTC)
	if !fc.Now().Equal(expectedAdvanced) {
		t.Fatalf("después de avanzar esperado %v, obtuvo %v", expectedAdvanced, fc.Now())
	}

	if fc.Since(base) != 30*time.Minute {
		t.Fatalf("Since esperado 30m, obtuvo %v", fc.Since(base))
	}

	newTime := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	fc.Set(newTime)
	if !fc.Now().Equal(newTime) {
		t.Fatalf("Set esperado %v, obtuvo %v", newTime, fc.Now())
	}
}

// internal/bench/report/stats_test.go
package report

import (
	"math"
	"testing"
)

func TestBootstrapCI95_KnownDistribution(t *testing.T) {
	xs := make([]float64, 100)
	for i := range xs {
		xs[i] = 100
	}
	mean, lo, hi := BootstrapCI95(xs, 1000, 42)
	if mean != 100 {
		t.Errorf("mean: want 100, got %v", mean)
	}
	if lo != 100 || hi != 100 {
		t.Errorf("constant samples should give zero-width CI, got [%v, %v]", lo, hi)
	}
}

func TestBootstrapCI95_VariedDistribution(t *testing.T) {
	xs := []float64{10, 12, 14, 16, 18, 20, 22, 24, 26, 28}
	mean, lo, hi := BootstrapCI95(xs, 1000, 42)
	if math.Abs(mean-19) > 0.001 {
		t.Errorf("mean: want 19, got %v", mean)
	}
	if !(lo < mean && mean < hi) {
		t.Errorf("CI should bracket mean, got [%v, %v] mean %v", lo, hi, mean)
	}
}

func TestBootstrapCI95_EmptyReturnsNaN(t *testing.T) {
	mean, lo, hi := BootstrapCI95(nil, 100, 0)
	if !math.IsNaN(mean) || !math.IsNaN(lo) || !math.IsNaN(hi) {
		t.Errorf("empty samples should yield NaN, got mean=%v lo=%v hi=%v", mean, lo, hi)
	}
}

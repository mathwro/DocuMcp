// internal/bench/report/stats.go
package report

import (
	"math"
	"math/rand"
	"sort"
)

// BootstrapCI95 returns the mean of xs and a 95% percentile bootstrap CI computed
// from `resamples` resamples. seed makes the result deterministic for tests.
// Returns NaN for all three values if xs is empty.
func BootstrapCI95(xs []float64, resamples int, seed int64) (mean, lo, hi float64) {
	if len(xs) == 0 {
		nan := math.NaN()
		return nan, nan, nan
	}
	mean = meanOf(xs)
	if resamples <= 0 {
		return mean, mean, mean
	}

	rng := rand.New(rand.NewSource(seed))
	means := make([]float64, resamples)
	tmp := make([]float64, len(xs))
	for i := 0; i < resamples; i++ {
		for j := range tmp {
			tmp[j] = xs[rng.Intn(len(xs))]
		}
		means[i] = meanOf(tmp)
	}
	sort.Float64s(means)
	lo = means[int(0.025*float64(resamples))]
	hi = means[int(0.975*float64(resamples))]
	return mean, lo, hi
}

func meanOf(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

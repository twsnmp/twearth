package analysis

import (
	"fmt"
	"math"
	"sort"
)

func RunStatistical(req CorrelationRequest, pairs []TimePairPoint, granularity string, warnings []string) (CorrelationResult, error) {
	xs := make([]float64, len(pairs))
	ys := make([]float64, len(pairs))
	for i, p := range pairs {
		xs[i] = p.X
		ys[i] = p.Y
	}

	res := CorrelationResult{
		Algorithm:   req.Algorithm,
		Granularity: granularity,
		SampleCount: len(pairs),
		Warnings:    warnings,
		TimeSeries:  pairs,
	}

	switch req.Algorithm {
	case "pearson":
		r, r2, pval, slope, intercept := pearsonStats(xs, ys)
		res.Metrics = CorrelationMetrics{
			Correlation: r,
			R2:          r2,
			PValue:      pval,
			Slope:       slope,
			Intercept:   intercept,
		}
		res.Scatter = buildScatter(pairs)
		res.RegressionLine = buildRegressionLine(xs, slope, intercept)
		return res, nil

	case "spearman":
		rho, pval := spearmanStats(xs, ys)
		res.Metrics = CorrelationMetrics{
			Correlation: rho,
			R2:          rho * rho,
			PValue:      pval,
		}
		res.Scatter = buildScatter(pairs)
		return res, nil

	case "cross_correlation":
		maxLag := req.Params.MaxLag
		if maxLag <= 0 {
			switch granularity {
			case "hourly":
				maxLag = 24
			case "daily":
				maxLag = 30
			default:
				maxLag = 12
			}
		}
		if maxLag > len(pairs)-3 {
			maxLag = len(pairs) - 3
		}
		if maxLag < 1 {
			return res, fmt.Errorf("insufficient points (%d) for cross-correlation", len(pairs))
		}

		profile := make([]LagPoint, 0, 2*maxLag+1)
		bestLag := 0
		bestCorr := 0.0
		bestAbs := -1.0

		for lag := -maxLag; lag <= maxLag; lag++ {
			shifted := shiftLagPairs(pairs, lag)
			if len(shifted) < 3 {
				continue
			}
			sx := make([]float64, len(shifted))
			sy := make([]float64, len(shifted))
			for i, p := range shifted {
				sx[i] = p.X
				sy[i] = p.Y
			}
			r, _, _, _, _ := pearsonStats(sx, sy)
			profile = append(profile, LagPoint{
				Lag:         lag,
				Correlation: r,
				SampleCount: len(shifted),
			})
			if math.Abs(r) > bestAbs {
				bestAbs = math.Abs(r)
				bestLag = lag
				bestCorr = r
			}
		}

		bestShifted := shiftLagPairs(pairs, bestLag)
		bx := make([]float64, len(bestShifted))
		by := make([]float64, len(bestShifted))
		for i, p := range bestShifted {
			bx[i] = p.X
			by[i] = p.Y
		}
		r, r2, pval, slope, intercept := pearsonStats(bx, by)

		res.SampleCount = len(bestShifted)
		res.TimeSeries = bestShifted
		res.LagProfile = profile
		res.Scatter = buildScatter(bestShifted)
		res.RegressionLine = buildRegressionLine(bx, slope, intercept)
		res.Metrics = CorrelationMetrics{
			Correlation:        r,
			R2:                 r2,
			PValue:             pval,
			Slope:              slope,
			Intercept:          intercept,
			BestLag:            bestLag,
			BestLagCorrelation: bestCorr,
		}
		return res, nil

	default:
		return res, fmt.Errorf("unknown statistical algorithm: %s", req.Algorithm)
	}
}

func buildScatter(pairs []TimePairPoint) []ScatterPoint {
	out := make([]ScatterPoint, len(pairs))
	for i, p := range pairs {
		out[i] = ScatterPoint{
			Label: p.Label,
			X:     p.X,
			Y:     p.Y,
		}
	}
	return out
}

func buildRegressionLine(xs []float64, slope, intercept float64) []ScatterPoint {
	if len(xs) == 0 {
		return nil
	}
	minX, maxX := xs[0], xs[0]
	for _, x := range xs[1:] {
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
	}
	return []ScatterPoint{
		{X: minX, Y: slope*minX + intercept},
		{X: maxX, Y: slope*maxX + intercept},
	}
}

func pearsonStats(xs, ys []float64) (r, r2, pValue, slope, intercept float64) {
	n := len(xs)
	if n < 2 || len(ys) != n {
		return 0, 0, 1, 0, 0
	}
	var sumX, sumY float64
	for i := 0; i < n; i++ {
		sumX += xs[i]
		sumY += ys[i]
	}
	meanX := sumX / float64(n)
	meanY := sumY / float64(n)

	var sxx, syy, sxy float64
	for i := 0; i < n; i++ {
		dx := xs[i] - meanX
		dy := ys[i] - meanY
		sxx += dx * dx
		syy += dy * dy
		sxy += dx * dy
	}
	if sxx <= 1e-15 || syy <= 1e-15 {
		return 0, 0, 1, 0, meanY
	}
	r = sxy / math.Sqrt(sxx*syy)
	if r > 1 {
		r = 1
	} else if r < -1 {
		r = -1
	}
	r2 = r * r
	slope = sxy / sxx
	intercept = meanY - slope*meanX
	pValue = correlationPValue(r, n)
	return r, r2, pValue, slope, intercept
}

func spearmanStats(xs, ys []float64) (rho, pValue float64) {
	n := len(xs)
	if n < 2 || len(ys) != n {
		return 0, 1
	}
	rx := averageRanks(xs)
	ry := averageRanks(ys)
	r, _, p, _, _ := pearsonStats(rx, ry)
	return r, p
}

func averageRanks(vals []float64) []float64 {
	n := len(vals)
	type idxVal struct {
		idx int
		val float64
	}
	sorted := make([]idxVal, n)
	for i, v := range vals {
		sorted[i] = idxVal{idx: i, val: v}
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].val < sorted[j].val
	})

	ranks := make([]float64, n)
	i := 0
	for i < n {
		j := i + 1
		for j < n && sorted[j].val == sorted[i].val {
			j++
		}
		avgRank := float64(i+j-1)/2.0 + 1.0
		for k := i; k < j; k++ {
			ranks[sorted[k].idx] = avgRank
		}
		i = j
	}
	return ranks
}

// correlationPValue computes the two-sided p-value for Pearson/Spearman r with n samples.
func correlationPValue(r float64, n int) float64 {
	if n <= 2 {
		return 1
	}
	if math.Abs(r) >= 1.0-1e-12 {
		return 0
	}
	df := float64(n - 2)
	t2 := (r * r * df) / (1.0 - r*r)
	x := df / (df + t2)
	return regularizedIncompleteBeta(df/2.0, 0.5, x)
}

func regularizedIncompleteBeta(a, b, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	lgA, _ := math.Lgamma(a)
	lgB, _ := math.Lgamma(b)
	lgAB, _ := math.Lgamma(a + b)
	bt := math.Exp(lgAB - lgA - lgB + a*math.Log(x) + b*math.Log(1.0-x))
	if x < (a+1.0)/(a+b+2.0) {
		return bt * betacf(a, b, x) / a
	}
	return 1.0 - bt*betacf(b, a, 1.0-x)/b
}

func betacf(a, b, x float64) float64 {
	const maxIter = 200
	const eps = 3e-7
	const fpMin = 1e-30

	qab := a + b
	qap := a + 1.0
	qam := a - 1.0
	c := 1.0
	d := 1.0 - qab*x/qap
	if math.Abs(d) < fpMin {
		d = fpMin
	}
	d = 1.0 / d
	h := d
	for m := 1; m <= maxIter; m++ {
		mf := float64(m)
		m2 := 2.0 * mf
		aa := mf * (b - mf) * x / ((qam + m2) * (a + m2))
		d = 1.0 + aa*d
		if math.Abs(d) < fpMin {
			d = fpMin
		}
		c = 1.0 + aa/c
		if math.Abs(c) < fpMin {
			c = fpMin
		}
		d = 1.0 / d
		h *= d * c
		aa = -(a + mf) * (qab + mf) * x / ((a + m2) * (qap + m2))
		d = 1.0 + aa*d
		if math.Abs(d) < fpMin {
			d = fpMin
		}
		c = 1.0 + aa/c
		if math.Abs(c) < fpMin {
			c = fpMin
		}
		d = 1.0 / d
		del := d * c
		h *= del
		if math.Abs(del-1.0) < eps {
			break
		}
	}
	return h
}

package analysis

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func makeSyntheticPairs(n int, fn func(i int) (x, y float64)) []TimePairPoint {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	out := make([]TimePairPoint, n)
	for i := 0; i < n; i++ {
		t := start.AddDate(0, i, 0)
		x, y := fn(i)
		out[i] = TimePairPoint{
			Timestamp: t.Format("200601021504"),
			Label:     t.Format("2006-01"),
			X:         x,
			Y:         y,
		}
	}
	return out
}

func TestPearsonAndSpearman(t *testing.T) {
	pairs := makeSyntheticPairs(40, func(i int) (float64, float64) {
		x := float64(i) * 0.25
		return x, 2.0*x + 1.0
	})

	res, err := AnalyzePairs(CorrelationRequest{Algorithm: "pearson"}, pairs, "monthly", nil)
	if err != nil {
		t.Fatalf("pearson failed: %v", err)
	}
	if math.Abs(res.Metrics.Correlation-1.0) > 1e-6 {
		t.Fatalf("expected r=1.0, got %f", res.Metrics.Correlation)
	}
	if math.Abs(res.Metrics.Slope-2.0) > 1e-6 || math.Abs(res.Metrics.Intercept-1.0) > 1e-6 {
		t.Fatalf("unexpected regression slope=%f intercept=%f", res.Metrics.Slope, res.Metrics.Intercept)
	}
	if res.Metrics.PValue > 1e-6 {
		t.Fatalf("expected near-zero pValue, got %g", res.Metrics.PValue)
	}

	sp, err := AnalyzePairs(CorrelationRequest{Algorithm: "spearman"}, pairs, "monthly", nil)
	if err != nil {
		t.Fatalf("spearman failed: %v", err)
	}
	if math.Abs(sp.Metrics.Correlation-1.0) > 1e-6 {
		t.Fatalf("expected rho=1.0, got %f", sp.Metrics.Correlation)
	}
}

func TestCrossCorrelationDetectsLag(t *testing.T) {
	n := 60
	xs := make([]float64, n)
	for i := 0; i < n; i++ {
		xs[i] = math.Sin(float64(i)*0.45) + 0.3*math.Cos(float64(i)*0.9)
	}
	pairs := makeSyntheticPairs(n, func(i int) (float64, float64) {
		y := 0.0
		if i >= 3 {
			y = xs[i-3]
		}
		return xs[i], y
	})

	res, err := AnalyzePairs(CorrelationRequest{
		Algorithm: "cross_correlation",
		Params:    AlgorithmParams{MaxLag: 6},
	}, pairs, "monthly", nil)
	if err != nil {
		t.Fatalf("cross_correlation failed: %v", err)
	}
	if res.Metrics.BestLag != 3 {
		t.Fatalf("expected bestLag=3, got %d (corr=%f)", res.Metrics.BestLag, res.Metrics.BestLagCorrelation)
	}
	if res.Metrics.BestLagCorrelation < 0.9 {
		t.Fatalf("expected strong best lag correlation, got %f", res.Metrics.BestLagCorrelation)
	}
}

func TestDeseasonalizeRemovesAnnualCycle(t *testing.T) {
	// 48 months: pure identical annual sine wave in both X and Y.
	pairs := makeSyntheticPairs(48, func(i int) (float64, float64) {
		season := math.Sin(2 * math.Pi * float64(i%12) / 12.0)
		return 25.0 + 3.0*season, 15.0 + 10.0*season
	})
	processed, warns := preprocessPairs(pairs, "monthly", PreprocessOptions{Deseasonalize: true}, true)
	if len(warns) > 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	for i, p := range processed {
		if math.Abs(p.X) > 1e-9 || math.Abs(p.Y) > 1e-9 {
			t.Fatalf("point %d not deseasonalized: x=%g y=%g", i, p.X, p.Y)
		}
	}
}

func TestTensaiMLPNonlinearRegression(t *testing.T) {
	n := 80
	xs := make([]float64, n)
	for i := 0; i < n; i++ {
		xs[i] = math.Sin(float64(i) * 0.3)
	}
	pairs := makeSyntheticPairs(n, func(i int) (float64, float64) {
		prev := xs[i]
		if i > 0 {
			prev = xs[i-1]
		}
		return xs[i], prev*prev + 0.5*xs[i]
	})

	res, err := AnalyzePairs(CorrelationRequest{
		Algorithm: "tensai_mlp",
		Params: AlgorithmParams{
			LagWindow:    2,
			HiddenSize:   16,
			Epochs:       250,
			LearningRate: 0.02,
			Activation:   "tanh",
			TrainRatio:   0.8,
		},
	}, pairs, "monthly", nil)
	if err != nil {
		t.Fatalf("tensai_mlp failed: %v", err)
	}
	if res.Metrics.R2 < 0.7 {
		t.Fatalf("expected MLP R2 >= 0.7, got %f (corr=%f)", res.Metrics.R2, res.Metrics.Correlation)
	}
	if len(res.LossHistory) == 0 {
		t.Fatalf("expected non-empty loss history")
	}
	if len(res.FeatureImportances) != 3 {
		t.Fatalf("expected 3 feature importances, got %d", len(res.FeatureImportances))
	}
}

func TestTensaiLSTMTimeSeries(t *testing.T) {
	n := 70
	xs := make([]float64, n)
	for i := 0; i < n; i++ {
		xs[i] = math.Sin(float64(i) * 0.35)
	}
	pairs := makeSyntheticPairs(n, func(i int) (float64, float64) {
		y := xs[i]
		if i >= 2 {
			y = 0.7*xs[i-1] + 0.3*xs[i-2]
		}
		return xs[i], y
	})

	res, err := AnalyzePairs(CorrelationRequest{
		Algorithm: "tensai_lstm",
		Params: AlgorithmParams{
			LagWindow:    3,
			HiddenSize:   10,
			Epochs:       120,
			LearningRate: 0.03,
			TrainRatio:   0.8,
		},
	}, pairs, "monthly", nil)
	if err != nil {
		t.Fatalf("tensai_lstm failed: %v", err)
	}
	if res.Metrics.Correlation < 0.7 {
		t.Fatalf("expected LSTM correlation >= 0.7, got %f (R2=%f)", res.Metrics.Correlation, res.Metrics.R2)
	}
}

func TestTensaiKNNRegimeClassification(t *testing.T) {
	pairs := makeSyntheticPairs(60, func(i int) (float64, float64) {
		x := float64(i%15) - 7.0
		y := 2.0*x + 20.0
		return x, y
	})

	res, err := AnalyzePairs(CorrelationRequest{
		Algorithm: "tensai_knn",
		Params: AlgorithmParams{
			LagWindow:  1,
			K:          3,
			Classes:    3,
			TrainRatio: 0.75,
		},
	}, pairs, "monthly", nil)
	if err != nil {
		t.Fatalf("tensai_knn failed: %v", err)
	}
	if res.Metrics.Accuracy < 0.7 {
		t.Fatalf("expected kNN accuracy >= 0.7, got %f", res.Metrics.Accuracy)
	}
	if res.ConfusionMatrix == nil || len(res.ConfusionMatrix.Matrix) != 3 {
		t.Fatalf("unexpected confusion matrix: %+v", res.ConfusionMatrix)
	}
	_ = fmt.Sprint(res.ConfusionMatrix.Labels)
}

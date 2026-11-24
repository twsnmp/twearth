package analysis

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"

	"github.com/mattn/tensai"
	"github.com/mattn/tensai/autograd"
	"github.com/mattn/tensai/knn"
	"github.com/mattn/tensai/layer"
	"github.com/mattn/tensai/loss"
	"github.com/mattn/tensai/metrics"
	"github.com/mattn/tensai/model"
	"github.com/mattn/tensai/optim"
	"github.com/mattn/tensai/rnn"
)

func RunTensaiML(req CorrelationRequest, pairs []TimePairPoint, granularity string, warnings []string) (CorrelationResult, error) {
	switch req.Algorithm {
	case "tensai_mlp":
		return runTensaiMLP(req, pairs, granularity, warnings)
	case "tensai_lstm":
		return runTensaiLSTM(req, pairs, granularity, warnings)
	case "tensai_knn":
		return runTensaiKNN(req, pairs, granularity, warnings)
	default:
		return CorrelationResult{}, fmt.Errorf("unknown tensai ML algorithm: %s", req.Algorithm)
	}
}

type mlWindowSample struct {
	pairIdx int
	xSeq    []float64 // ordered oldest -> newest: [X[t-W], ..., X[t]]
	y       float64
}

func buildWindowSamples(pairs []TimePairPoint, lagWindow int) []mlWindowSample {
	if lagWindow < 0 {
		lagWindow = 0
	}
	if lagWindow >= len(pairs)-4 {
		lagWindow = max(0, len(pairs)-5)
	}
	out := make([]mlWindowSample, 0, len(pairs)-lagWindow)
	for t := lagWindow; t < len(pairs); t++ {
		seq := make([]float64, lagWindow+1)
		for k := 0; k <= lagWindow; k++ {
			// seq[0] = X[t-lagWindow], ..., seq[lagWindow] = X[t]
			seq[k] = pairs[t-lagWindow+k].X
		}
		out = append(out, mlWindowSample{
			pairIdx: t,
			xSeq:    seq,
			y:       pairs[t].Y,
		})
	}
	return out
}

func splitTrainTestCount(n int, ratio float64) (trainN, testN int) {
	if ratio <= 0.1 || ratio >= 0.95 {
		ratio = 0.8
	}
	trainN = int(math.Round(float64(n) * ratio))
	if trainN < 4 {
		trainN = min(4, n-2)
	}
	if trainN > n-2 {
		trainN = n - 2
	}
	testN = n - trainN
	return trainN, testN
}

func activationLayer(name string) layer.Layer {
	switch name {
	case "relu":
		return &layer.ReLU{}
	case "leaky_relu":
		return layer.NewLeakyReLU(0.1)
	case "gelu":
		return &layer.GELU{}
	default:
		return &layer.Tanh{}
	}
}

func calcMeanStd(vals []float64) (mean, std float64) {
	if len(vals) == 0 {
		return 0, 1
	}
	for _, v := range vals {
		mean += v
	}
	mean /= float64(len(vals))
	var ss float64
	for _, v := range vals {
		d := v - mean
		ss += d * d
	}
	std = math.Sqrt(ss / float64(len(vals)))
	if std < 1e-12 {
		std = 1
	}
	return mean, std
}

func evalRegressionMetrics(actual, pred []float64) (r, r2, rmse, mae float64) {
	n := len(actual)
	if n == 0 || len(pred) != n {
		return 0, 0, 0, 0
	}
	var sumSq, sumAbs, sumY float64
	for i := 0; i < n; i++ {
		err := pred[i] - actual[i]
		sumSq += err * err
		sumAbs += math.Abs(err)
		sumY += actual[i]
	}
	rmse = math.Sqrt(sumSq / float64(n))
	mae = sumAbs / float64(n)

	meanY := sumY / float64(n)
	var ssTot float64
	for _, y := range actual {
		d := y - meanY
		ssTot += d * d
	}
	if ssTot > 1e-12 {
		r2 = 1.0 - sumSq/ssTot
	}
	r, _, _, _, _ = pearsonStats(actual, pred)
	return r, r2, rmse, mae
}

func runTensaiMLP(req CorrelationRequest, pairs []TimePairPoint, granularity string, warnings []string) (CorrelationResult, error) {
	lagWin := req.Params.LagWindow
	if lagWin <= 0 {
		lagWin = 3
	}
	hidden := req.Params.HiddenSize
	if hidden <= 0 {
		hidden = 16
	}
	if hidden > 128 {
		hidden = 128
	}
	epochs := req.Params.Epochs
	if epochs <= 0 {
		epochs = 200
	}
	if epochs > 1000 {
		epochs = 1000
	}
	lr := req.Params.LearningRate
	if lr <= 0 || lr > 1 {
		lr = 0.01
	}

	samples := buildWindowSamples(pairs, lagWin)
	if len(samples) < 10 {
		return CorrelationResult{}, fmt.Errorf("insufficient samples (%d) for tensai MLP (need >= 10)", len(samples))
	}
	trainN, testN := splitTrainTestCount(len(samples), req.Params.TrainRatio)

	// Fit normalization stats strictly on training slice to avoid leakage.
	trainXFlat := make([]float64, 0, trainN*(lagWin+1))
	trainYVals := make([]float64, trainN)
	for i := 0; i < trainN; i++ {
		trainXFlat = append(trainXFlat, samples[i].xSeq...)
		trainYVals[i] = samples[i].y
	}
	meanX, stdX := calcMeanStd(trainXFlat)
	meanY, stdY := calcMeanStd(trainYVals)

	inCols := lagWin + 1
	toMatrices := func(subset []mlWindowSample) (*tensai.Matrix, *tensai.Matrix, error) {
		xd := make([]tensai.Float, len(subset)*inCols)
		yd := make([]tensai.Float, len(subset))
		for r, s := range subset {
			for c, v := range s.xSeq {
				xd[r*inCols+c] = tensai.Float((v - meanX) / stdX)
			}
			yd[r] = tensai.Float((s.y - meanY) / stdY)
		}
		xm, err := tensai.NewMatrixFromSlice(len(subset), inCols, xd)
		if err != nil {
			return nil, nil, err
		}
		ym, err := tensai.NewMatrixFromSlice(len(subset), 1, yd)
		if err != nil {
			return nil, nil, err
		}
		return xm, ym, nil
	}

	trainX, trainY, err := toMatrices(samples[:trainN])
	if err != nil {
		return CorrelationResult{}, err
	}
	testX, testY, err := toMatrices(samples[trainN:])
	if err != nil {
		return CorrelationResult{}, err
	}
	allX, _, err := toMatrices(samples)
	if err != nil {
		return CorrelationResult{}, err
	}

	net := model.NewSequential().
		Add(layer.NewDense(hidden)).
		Add(activationLayer(req.Params.Activation)).
		Add(layer.NewDense(1))

	if err := net.Compile(inCols, loss.MeanSquaredError{}, optim.NewAdamW(tensai.Float(lr), 1e-4)); err != nil {
		return CorrelationResult{}, fmt.Errorf("tensai mlp compile: %w", err)
	}

	lossHist := make([]LossPoint, 0, epochs)
	mse := loss.MeanSquaredError{}
	logStep := max(1, epochs/50)
	for ep := 1; ep <= epochs; ep++ {
		trLoss, err := net.FitStep(trainX, trainY)
		if err != nil {
			return CorrelationResult{}, fmt.Errorf("tensai mlp train epoch %d: %w", ep, err)
		}
		if ep == 1 || ep%logStep == 0 || ep == epochs {
			predTest, err := net.Predict(testX)
			if err != nil {
				return CorrelationResult{}, err
			}
			teLoss, _, err := mse.Loss(predTest, testY)
			if err != nil {
				return CorrelationResult{}, err
			}
			lossHist = append(lossHist, LossPoint{
				Epoch:     ep,
				TrainLoss: float64(trLoss),
				TestLoss:  float64(teLoss),
			})
		}
	}

	allPredNorm, err := net.Predict(allX)
	if err != nil {
		return CorrelationResult{}, err
	}

	outSeries := make([]TimePairPoint, len(samples))
	testActual := make([]float64, testN)
	testPred := make([]float64, testN)
	for i, s := range samples {
		p := pairs[s.pairIdx]
		predVal := float64(allPredNorm.Data[i])*stdY + meanY
		isTest := i >= trainN
		if isTest {
			testActual[i-trainN] = p.Y
			testPred[i-trainN] = predVal
		}
		pv := predVal
		outSeries[i] = TimePairPoint{
			Timestamp:  p.Timestamp,
			Label:      p.Label,
			X:          p.X,
			Y:          p.Y,
			PredictedY: &pv,
			IsTest:     isTest,
		}
	}

	r, r2, rmse, mae := evalRegressionMetrics(testActual, testPred)

	// Permutation importance on testX for each lag column.
	importances := computeMLPPermutationImportance(net, testX, testY, lagWin)

	scatter := make([]ScatterPoint, testN)
	for i := 0; i < testN; i++ {
		scatter[i] = ScatterPoint{
			Label: outSeries[trainN+i].Label,
			X:     testActual[i],
			Y:     testPred[i],
		}
	}

	return CorrelationResult{
		Algorithm:   req.Algorithm,
		Granularity: granularity,
		SampleCount: len(samples),
		TrainCount:  trainN,
		TestCount:   testN,
		Warnings:    warnings,
		Metrics: CorrelationMetrics{
			Correlation: r,
			R2:          r2,
			PValue:      correlationPValue(r, testN),
			RMSE:        rmse,
			MAE:         mae,
		},
		TimeSeries:         outSeries,
		Scatter:            scatter,
		LossHistory:        lossHist,
		FeatureImportances: importances,
	}, nil
}

func computeMLPPermutationImportance(net *model.Sequential, testX, testY *tensai.Matrix, lagWin int) []FeatureImportance {
	basePred, err := net.Predict(testX)
	if err != nil {
		return nil
	}
	mse := loss.MeanSquaredError{}
	baseLoss, _, err := mse.Loss(basePred, testY)
	if err != nil {
		return nil
	}

	rng := rand.New(rand.NewPCG(42, 7))
	importances := make([]FeatureImportance, testX.Cols)
	var sumImp float64

	permData := make([]tensai.Float, len(testX.Data))
	for c := 0; c < testX.Cols; c++ {
		copy(permData, testX.Data)
		colVals := make([]tensai.Float, testX.Rows)
		for r := 0; r < testX.Rows; r++ {
			colVals[r] = permData[r*testX.Cols+c]
		}
		rng.Shuffle(len(colVals), func(i, j int) {
			colVals[i], colVals[j] = colVals[j], colVals[i]
		})
		for r := 0; r < testX.Rows; r++ {
			permData[r*testX.Cols+c] = colVals[r]
		}
		pm, err := tensai.NewMatrixFromSlice(testX.Rows, testX.Cols, permData)
		if err != nil {
			continue
		}
		pred, err := net.Predict(pm)
		if err != nil {
			continue
		}
		pLoss, _, err := mse.Loss(pred, testY)
		if err != nil {
			continue
		}
		diff := math.Max(0, float64(pLoss-baseLoss))
		lag := lagWin - c
		name := fmt.Sprintf("Lag %d", lag)
		if lag == 0 {
			name = "Lag 0 (Current)"
		}
		importances[c] = FeatureImportance{
			Name:       name,
			Lag:        lag,
			Importance: diff,
		}
		sumImp += diff
	}
	if sumImp > 1e-12 {
		for i := range importances {
			importances[i].Importance /= sumImp
		}
	}
	return importances
}

func runTensaiLSTM(req CorrelationRequest, pairs []TimePairPoint, granularity string, warnings []string) (CorrelationResult, error) {
	seqLen := req.Params.LagWindow
	if seqLen <= 0 {
		seqLen = 4
	}
	hidden := req.Params.HiddenSize
	if hidden <= 0 {
		hidden = 12
	}
	if hidden > 64 {
		hidden = 64
	}
	epochs := req.Params.Epochs
	if epochs <= 0 {
		epochs = 120
	}
	if epochs > 500 {
		epochs = 500
	}
	lr := req.Params.LearningRate
	if lr <= 0 || lr > 1 {
		lr = 0.02
	}

	samples := buildWindowSamples(pairs, seqLen)
	if len(samples) < 10 {
		return CorrelationResult{}, fmt.Errorf("insufficient samples (%d) for tensai LSTM (need >= 10)", len(samples))
	}
	trainN, testN := splitTrainTestCount(len(samples), req.Params.TrainRatio)

	trainXFlat := make([]float64, 0, trainN*(seqLen+1))
	trainYVals := make([]float64, trainN)
	for i := 0; i < trainN; i++ {
		trainXFlat = append(trainXFlat, samples[i].xSeq...)
		trainYVals[i] = samples[i].y
	}
	meanX, stdX := calcMeanStd(trainXFlat)
	meanY, stdY := calcMeanStd(trainYVals)

	stepsCount := seqLen + 1
	buildSteps := func(subset []mlWindowSample) ([]*tensai.Matrix, *tensai.Matrix, error) {
		steps := make([]*tensai.Matrix, stepsCount)
		for t := 0; t < stepsCount; t++ {
			col := make([]tensai.Float, len(subset))
			for i, s := range subset {
				col[i] = tensai.Float((s.xSeq[t] - meanX) / stdX)
			}
			m, err := tensai.NewMatrixFromSlice(len(subset), 1, col)
			if err != nil {
				return nil, nil, err
			}
			steps[t] = m
		}
		yd := make([]tensai.Float, len(subset))
		for i, s := range subset {
			yd[i] = tensai.Float((s.y - meanY) / stdY)
		}
		ym, err := tensai.NewMatrixFromSlice(len(subset), 1, yd)
		if err != nil {
			return nil, nil, err
		}
		return steps, ym, nil
	}

	trainSteps, trainY, err := buildSteps(samples[:trainN])
	if err != nil {
		return CorrelationResult{}, err
	}
	testSteps, testY, err := buildSteps(samples[trainN:])
	if err != nil {
		return CorrelationResult{}, err
	}
	allSteps, _, err := buildSteps(samples)
	if err != nil {
		return CorrelationResult{}, err
	}

	rng := rand.New(rand.NewPCG(17, 29))
	cell := rnn.NewLSTMCell(1, hidden, rng)
	wOut := autograd.Param(tensai.RandomMatrix(hidden, 1, rng))
	bOut := autograd.Param(tensai.NewMatrix(1, 1))
	params := append(cell.Params(), wOut, bOut)
	trainer := autograd.NewTrainer(optim.NewAdam(tensai.Float(lr)), params...)

	forward := func(steps []*tensai.Matrix, batch int) *autograd.Node {
		h, c := cell.InitState(batch)
		for _, x := range steps {
			h, c = cell.Step(autograd.Input(x), h, c)
		}
		return h.MatMul(wOut).AddRow(bOut)
	}

	lossHist := make([]LossPoint, 0, epochs)
	logStep := max(1, epochs/50)
	for ep := 1; ep <= epochs; ep++ {
		trLoss := trainer.Step(forward(trainSteps, trainN).MSELoss(trainY.Tensor()))
		if ep == 1 || ep%logStep == 0 || ep == epochs {
			teLossNode := forward(testSteps, testN).MSELoss(testY.Tensor())
			lossHist = append(lossHist, LossPoint{
				Epoch:     ep,
				TrainLoss: float64(trLoss),
				TestLoss:  float64(teLossNode.Value().Data[0]),
			})
		}
	}

	allPredData := forward(allSteps, len(samples)).Value().Data
	outSeries := make([]TimePairPoint, len(samples))
	testActual := make([]float64, testN)
	testPred := make([]float64, testN)
	for i, s := range samples {
		p := pairs[s.pairIdx]
		predVal := float64(allPredData[i])*stdY + meanY
		isTest := i >= trainN
		if isTest {
			testActual[i-trainN] = p.Y
			testPred[i-trainN] = predVal
		}
		pv := predVal
		outSeries[i] = TimePairPoint{
			Timestamp:  p.Timestamp,
			Label:      p.Label,
			X:          p.X,
			Y:          p.Y,
			PredictedY: &pv,
			IsTest:     isTest,
		}
	}

	r, r2, rmse, mae := evalRegressionMetrics(testActual, testPred)
	scatter := make([]ScatterPoint, testN)
	for i := 0; i < testN; i++ {
		scatter[i] = ScatterPoint{
			Label: outSeries[trainN+i].Label,
			X:     testActual[i],
			Y:     testPred[i],
		}
	}

	return CorrelationResult{
		Algorithm:   req.Algorithm,
		Granularity: granularity,
		SampleCount: len(samples),
		TrainCount:  trainN,
		TestCount:   testN,
		Warnings:    warnings,
		Metrics: CorrelationMetrics{
			Correlation: r,
			R2:          r2,
			PValue:      correlationPValue(r, testN),
			RMSE:        rmse,
			MAE:         mae,
		},
		TimeSeries:  outSeries,
		Scatter:     scatter,
		LossHistory: lossHist,
	}, nil
}

func runTensaiKNN(req CorrelationRequest, pairs []TimePairPoint, granularity string, warnings []string) (CorrelationResult, error) {
	lagWin := req.Params.LagWindow
	if lagWin < 0 {
		lagWin = 2
	}
	kVal := req.Params.K
	if kVal <= 0 {
		kVal = 5
	}
	classes := req.Params.Classes
	if classes != 2 && classes != 3 {
		classes = 3
	}

	samples := buildWindowSamples(pairs, lagWin)
	if len(samples) < 12 {
		return CorrelationResult{}, fmt.Errorf("insufficient samples (%d) for tensai kNN (need >= 12)", len(samples))
	}
	trainN, testN := splitTrainTestCount(len(samples), req.Params.TrainRatio)
	if kVal > trainN {
		kVal = max(1, trainN/2)
	}

	// Compute quantile thresholds from training Y values.
	trainY := make([]float64, trainN)
	trainXFlat := make([]float64, 0, trainN*(lagWin+1))
	for i := 0; i < trainN; i++ {
		trainY[i] = samples[i].y
		trainXFlat = append(trainXFlat, samples[i].xSeq...)
	}
	meanX, stdX := calcMeanStd(trainXFlat)

	sortedY := make([]float64, len(trainY))
	copy(sortedY, trainY)
	sort.Float64s(sortedY)

	var thresholds []float64
	var classLabels []string
	if classes == 2 {
		thresholds = []float64{sortedY[len(sortedY)/2]}
		classLabels = []string{"Low", "High"}
	} else {
		thresholds = []float64{
			sortedY[len(sortedY)/3],
			sortedY[(2*len(sortedY))/3],
		}
		classLabels = []string{"Low", "Normal", "High"}
	}

	assignClass := func(y float64) int {
		for idx, th := range thresholds {
			if y <= th {
				return idx
			}
		}
		return len(thresholds)
	}

	inCols := lagWin + 1
	buildKNNMatrices := func(subset []mlWindowSample) (*tensai.Matrix, *tensai.Matrix, error) {
		xd := make([]tensai.Float, len(subset)*inCols)
		yd := make([]tensai.Float, len(subset))
		for r, s := range subset {
			for c, v := range s.xSeq {
				xd[r*inCols+c] = tensai.Float((v - meanX) / stdX)
			}
			yd[r] = tensai.Float(assignClass(s.y))
		}
		xm, err := tensai.NewMatrixFromSlice(len(subset), inCols, xd)
		if err != nil {
			return nil, nil, err
		}
		ym, err := tensai.NewMatrixFromSlice(len(subset), 1, yd)
		if err != nil {
			return nil, nil, err
		}
		return xm, ym, nil
	}

	trainXMat, trainYMat, err := buildKNNMatrices(samples[:trainN])
	if err != nil {
		return CorrelationResult{}, err
	}
	testXMat, testYMat, err := buildKNNMatrices(samples[trainN:])
	if err != nil {
		return CorrelationResult{}, err
	}
	allXMat, _, err := buildKNNMatrices(samples)
	if err != nil {
		return CorrelationResult{}, err
	}

	clf := knn.New(kVal)
	if err := clf.Fit(trainXMat, trainYMat); err != nil {
		return CorrelationResult{}, fmt.Errorf("tensai knn fit: %w", err)
	}

	evalRes, err := metrics.Evaluate(clf, testXMat, testYMat)
	if err != nil {
		return CorrelationResult{}, fmt.Errorf("tensai knn evaluate: %w", err)
	}
	allPred, err := clf.Predict(allXMat)
	if err != nil {
		return CorrelationResult{}, err
	}

	outSeries := make([]TimePairPoint, len(samples))
	for i, s := range samples {
		p := pairs[s.pairIdx]
		predCls := float64(allPred.ArgmaxRow(i))
		outSeries[i] = TimePairPoint{
			Timestamp:  p.Timestamp,
			Label:      p.Label,
			X:          p.X,
			Y:          p.Y,
			PredictedY: &predCls,
			IsTest:     i >= trainN,
		}
	}

	return CorrelationResult{
		Algorithm:   req.Algorithm,
		Granularity: granularity,
		SampleCount: len(samples),
		TrainCount:  trainN,
		TestCount:   testN,
		Warnings:    warnings,
		Metrics: CorrelationMetrics{
			Accuracy: evalRes.Accuracy(),
		},
		TimeSeries: outSeries,
		ConfusionMatrix: &ConfusionMatrixResult{
			Labels: classLabels,
			Matrix: evalRes.Confusion,
		},
	}, nil
}

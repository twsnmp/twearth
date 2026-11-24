package analysis

// SeriesSpec identifies a time-series target (weather or marine location + metric).
type SeriesSpec struct {
	Kind       string `json:"kind"` // "weather" | "marine"
	LocationID string `json:"locationId"`
	Metric     string `json:"metric"`
	Label      string `json:"label,omitempty"`
}

// PreprocessOptions defines transformations applied before correlation/ML analysis.
type PreprocessOptions struct {
	Deseasonalize   bool `json:"deseasonalize"`
	MovingAvgWindow int  `json:"movingAvgWindow"`
	Difference      bool `json:"difference"`
	Standardize     bool `json:"standardize"`
	Lag             int  `json:"lag"` // positive lag shifts X earlier: pairs X[t-lag] with Y[t]
}

// AlgorithmParams configures statistical and tensai ML algorithm hyperparameters.
type AlgorithmParams struct {
	MaxLag       int     `json:"maxLag"`       // cross_correlation sweep [-MaxLag, +MaxLag]
	LagWindow    int     `json:"lagWindow"`    // tensai_mlp / tensai_lstm / tensai_knn past window size
	HiddenSize   int     `json:"hiddenSize"`   // tensai_mlp / tensai_lstm hidden units
	Epochs       int     `json:"epochs"`       // tensai_mlp / tensai_lstm training epochs
	LearningRate float64 `json:"learningRate"` // optimizer step size
	Activation   string  `json:"activation"`   // "tanh" | "relu" | "leaky_relu"
	TrainRatio   float64 `json:"trainRatio"`   // chronological train/test split ratio (e.g. 0.8)
	K            int     `json:"k"`            // tensai_knn neighbors
	Classes      int     `json:"classes"`      // tensai_knn quantile classes (2 or 3)
}

// CorrelationRequest is the input payload for AnalyzeCorrelation.
type CorrelationRequest struct {
	SeriesX     SeriesSpec        `json:"seriesX"`
	SeriesY     SeriesSpec        `json:"seriesY"`
	RangeKey    string            `json:"rangeKey"`    // "30d" | "90d" | "1y" | "3y" | "5y" | "10y" | "30y" | "all"
	Granularity string            `json:"granularity"` // "daily" | "monthly" | "hourly" | "auto"
	Preprocess  PreprocessOptions `json:"preprocess"`
	Algorithm   string            `json:"algorithm"` // "pearson" | "spearman" | "cross_correlation" | "tensai_mlp" | "tensai_lstm" | "tensai_knn"
	Params      AlgorithmParams   `json:"params"`
}

// CorrelationMetrics summarizes statistical or ML evaluation metrics.
type CorrelationMetrics struct {
	Correlation        float64 `json:"correlation"`
	R2                 float64 `json:"r2"`
	PValue             float64 `json:"pValue"`
	Slope              float64 `json:"slope"`
	Intercept          float64 `json:"intercept"`
	BestLag            int     `json:"bestLag"`
	BestLagCorrelation float64 `json:"bestLagCorrelation"`
	RMSE               float64 `json:"rmse"`
	MAE                float64 `json:"mae"`
	Accuracy           float64 `json:"accuracy"`
}

// TimePairPoint holds aligned X, Y, and optional ML-predicted Y values over time.
type TimePairPoint struct {
	Timestamp  string   `json:"timestamp"`
	Label      string   `json:"label"`
	X          float64  `json:"x"`
	Y          float64  `json:"y"`
	PredictedY *float64 `json:"predictedY,omitempty"`
	IsTest     bool     `json:"isTest,omitempty"`
}

// ScatterPoint represents a single (X, Y) observation for scatter / regression plotting.
type ScatterPoint struct {
	Label string  `json:"label,omitempty"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
}

// LagPoint represents correlation at a specific time lag.
type LagPoint struct {
	Lag         int     `json:"lag"`
	Correlation float64 `json:"correlation"`
	SampleCount int     `json:"sampleCount"`
}

// LossPoint records training/test loss per epoch for tensai models.
type LossPoint struct {
	Epoch     int     `json:"epoch"`
	TrainLoss float64 `json:"trainLoss"`
	TestLoss  float64 `json:"testLoss"`
}

// FeatureImportance represents permutation importance for an input feature/lag.
type FeatureImportance struct {
	Name       string  `json:"name"`
	Lag        int     `json:"lag"`
	Importance float64 `json:"importance"`
}

// ConfusionMatrixResult summarizes classification results for tensai_knn.
type ConfusionMatrixResult struct {
	Labels []string `json:"labels"`
	Matrix [][]int  `json:"matrix"`
}

// CorrelationResult is returned by AnalyzeCorrelation.
type CorrelationResult struct {
	Algorithm          string                 `json:"algorithm"`
	Granularity        string                 `json:"granularity"`
	SampleCount        int                    `json:"sampleCount"`
	TrainCount         int                    `json:"trainCount,omitempty"`
	TestCount          int                    `json:"testCount,omitempty"`
	Warnings           []string               `json:"warnings,omitempty"`
	Metrics            CorrelationMetrics     `json:"metrics"`
	TimeSeries         []TimePairPoint        `json:"timeSeries"`
	Scatter            []ScatterPoint         `json:"scatter,omitempty"`
	RegressionLine     []ScatterPoint         `json:"regressionLine,omitempty"`
	LagProfile         []LagPoint             `json:"lagProfile,omitempty"`
	LossHistory        []LossPoint            `json:"lossHistory,omitempty"`
	FeatureImportances []FeatureImportance    `json:"featureImportances,omitempty"`
	ConfusionMatrix    *ConfusionMatrixResult `json:"confusionMatrix,omitempty"`
}

// CatalogLocation describes a selectable weather or marine location.
type CatalogLocation struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // "weather" | "marine"
	Name     string `json:"name"`
	NameEn   string `json:"nameEn,omitempty"`
	Category string `json:"category,omitempty"`
	Region   string `json:"region,omitempty"`
}

// CatalogMetric describes a selectable metric for weather or marine series.
type CatalogMetric struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // "weather" | "marine"
	Unit string `json:"unit"`
}

// SeriesCatalog lists all available locations and metrics for correlation analysis.
type SeriesCatalog struct {
	Locations []CatalogLocation `json:"locations"`
	Metrics   []CatalogMetric   `json:"metrics"`
}

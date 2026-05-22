package advisor

// AdviceResult 存放最终的治理建议与经济核算结果
type AdviceResult struct {
	Namespace  string
	Deployment string
	AppGroup   string

	OldCPURequest  int
	OldCPULimit    int
	OldMemRequest  int
	OldMemLimit    int
	CPUUsageMax    float64
	CPUUsageAvg    float64
	MemUsageMax    float64
	ThrottleSecond float64
	AvgRPS         float64

	NewCPURequest int
	NewMemRequest int

	CurrentCost     float64
	RecommendedCost float64
	ActualCost      float64
	MonthlySaving   float64

	IsGatewayUser bool
	RpsWeight     float64
	GwShareCost   float64
	FullUnitCost  float64
	TotalCost     float64

	Efficiency float64
	RPSDensity float64

	PriorityScore float64

	Action    string
	Reason    string
	RiskLevel string

	IsHighRisk    bool
	IsInefficient bool
	IsProtected   bool
	IsBlackHole   bool
}

// EfficiencyResult 存放流量效能分析结果
type EfficiencyResult struct {
	Namespace   string
	ServiceName string
	AppGroup    string

	CurrentCPU int
	CurrentMem int
	Replicas   int

	UsageCPUP95 float64
	UsageCPUAvg float64
	UsageMemMax float64
	AvgRPS      float64

	TrafficDensity     float64
	TrafficDensityRank string

	RecCPU int
	RecMem int

	CurrentCost   float64
	RecCost       float64
	ActualCost    float64
	MonthlySaving float64
	WasteAmount   float64
	WasteRatio    float64

	Action string
	Reason string
}

// ResourceBlackHole 存放资源黑洞数据
type ResourceBlackHole struct {
	ServiceName string
	Namespace   string
	CurrentCost float64
	ActualCost  float64
	WasteRatio  float64
	WasteAmount float64
}

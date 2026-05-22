package health

// HealthMetrics 存放从 Traefik 采集的健康指标
type HealthMetrics struct {
	ServiceName string
	Namespace   string

	RPS          float64
	Error5xxRate float64
	Error4xxRate float64
	P99Latency   float64

	MonthlyCost    float64
	CPUUtilization float64

	RestartCount int

	PodCost     float64
	GwShareCost float64
	TotalCost   float64
}

// HealthStatus 健康检查结果
type HealthStatus struct {
	ServiceName string
	Namespace   string

	RPS          float64
	Error5xxRate float64
	Error4xxRate float64
	P99Latency   float64

	HealthCode string
	StatusIcon string

	InvalidSpend     float64
	WastedSpendTotal float64

	Diagnosis string
	Action    string
}

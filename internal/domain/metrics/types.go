package metrics

// ServiceMetrics 存放从 Prometheus 采集的服务指标
type ServiceMetrics struct {
	Namespace  string
	Deployment string
	AppGroup   string

	CPURequest float64
	CPULimit   float64
	MemRequest float64
	MemLimit   float64
	Replicas   int

	CPUUsageMax    float64
	CPUUsageAvg    float64
	MemUsageMax    float64
	ThrottleSecond float64
	AvgRPS         float64

	CurrentCost float64
	ActualCost  float64
	PodCost     float64

	IsGatewayUser bool
	RpsWeight     float64
	GwShareCost   float64
	FullUnitCost  float64
	TotalCost     float64
}

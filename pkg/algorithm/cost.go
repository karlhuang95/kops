package algorithm

import platformpricing "kops/internal/platform/pricing"
import "kops/pkg/config"
import "kops/pkg/model"

type GatewayCostCalculator = platformpricing.GatewayCostCalculator
type CostBreakdown = platformpricing.CostBreakdown

func NewGatewayCostCalculator(cfg *config.GlobalConfig) *GatewayCostCalculator {
	return platformpricing.NewGatewayCostCalculator(cfg)
}

func IsTaskService(cfg *config.GlobalConfig, deployment string) bool {
	return platformpricing.IsTaskService(cfg, deployment)
}

func UnitPrices(cfg *config.GlobalConfig) (cpuUnitPrice, memUnitPrice float64) {
	return platformpricing.UnitPrices(cfg)
}

func NormalizeReplicas(replicas int) int {
	return platformpricing.NormalizeReplicas(replicas)
}

func PodMonthlyCost(cfg *config.GlobalConfig, reqCPU, reqMem float64, replicas int) CostBreakdown {
	return platformpricing.PodMonthlyCost(cfg, reqCPU, reqMem, replicas)
}

func WithGatewayCost(base CostBreakdown, gatewayCost float64) CostBreakdown {
	return platformpricing.WithGatewayCost(base, gatewayCost)
}

func CalculateKopsMetrics(calc *GatewayCostCalculator, s *model.ServiceMetrics, totalTrafficRPS float64, gwTotalCost float64) {
	calc.CalculateKopsMetrics(s, totalTrafficRPS, gwTotalCost)
}

func Quantize(val float64, step int, min int) int {
	return platformpricing.Quantize(val, step, min)
}

func RecommendCPU(cfg *config.GlobalConfig, cpuP95, cpuRequest float64) int {
	return platformpricing.RecommendCPU(cfg, cpuP95, cpuRequest)
}

func RecommendMemory(cfg *config.GlobalConfig, memMax float64) int {
	return platformpricing.RecommendMemory(cfg, memMax)
}

func CalculateTotalTrafficRPS(cfg *config.GlobalConfig, metrics []model.ServiceMetrics) float64 {
	return platformpricing.CalculateTotalTrafficRPS(cfg, metrics)
}

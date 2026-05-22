package pricing

import (
	"math"
	"strings"

	"kops/pkg/config"
	"kops/pkg/model"
)

type GatewayCostCalculator struct {
	cfg *config.GlobalConfig
}

type CostBreakdown struct {
	CPUCost     float64
	MemCost     float64
	GatewayCost float64
	TotalCost   float64
}

func NewGatewayCostCalculator(cfg *config.GlobalConfig) *GatewayCostCalculator {
	return &GatewayCostCalculator{cfg: cfg}
}

func IsTaskService(cfg *config.GlobalConfig, deployment string) bool {
	if cfg == nil {
		return false
	}
	name := strings.ToLower(deployment)
	for _, pat := range cfg.Governance.TaskServicePatterns {
		if strings.Contains(name, strings.ToLower(pat)) {
			return true
		}
	}
	return false
}

func (gcc *GatewayCostCalculator) CalculateGatewayTotalCost() float64 {
	return gcc.cfg.GatewayCost.Price * float64(gcc.cfg.GatewayCost.Count)
}

func (gcc *GatewayCostCalculator) CalculateKopsMetrics(s *model.ServiceMetrics, totalTrafficRPS float64, gwTotalCost float64) {
	if IsTaskService(gcc.cfg, s.Deployment) {
		s.IsGatewayUser = false
		s.RpsWeight = 0
		s.GwShareCost = 0
	} else {
		s.IsGatewayUser = true
		if totalTrafficRPS > 0 {
			s.RpsWeight = s.AvgRPS / totalTrafficRPS
		} else {
			s.RpsWeight = 0
			s.GwShareCost = 0
			s.IsGatewayUser = false
		}
		if s.IsGatewayUser {
			s.GwShareCost = s.RpsWeight * gwTotalCost
		}
	}

	podCost := PodMonthlyCost(gcc.cfg, s.CPURequest, s.MemRequest, s.Replicas)
	s.CurrentCost = podCost.TotalCost
	s.PodCost = podCost.TotalCost

	monthlyTotalRequests := s.AvgRPS * 3600 * 24 * 30
	if monthlyTotalRequests > 0 {
		s.TotalCost = s.PodCost + s.GwShareCost
		s.FullUnitCost = (s.TotalCost / monthlyTotalRequests) * 10000
	} else {
		s.TotalCost = s.PodCost + s.GwShareCost
		s.FullUnitCost = s.TotalCost
	}
}

func UnitPrices(cfg *config.GlobalConfig) (cpuUnitPrice, memUnitPrice float64) {
	if cfg == nil {
		return 0, 0
	}
	if cfg.Cost.CPUCores > 0 {
		cpuUnitPrice = cfg.Cost.Price * cfg.Cost.ResourceWeight.CPU / float64(cfg.Cost.CPUCores)
	}
	if cfg.Cost.MemoryGB > 0 {
		memUnitPrice = (cfg.Cost.Price * cfg.Cost.ResourceWeight.Memory) /
			(float64(cfg.Cost.MemoryGB) * 1024.0)
	}
	return cpuUnitPrice, memUnitPrice
}

func NormalizeReplicas(replicas int) int {
	if replicas <= 0 {
		return 1
	}
	return replicas
}

func PodMonthlyCost(cfg *config.GlobalConfig, reqCPU, reqMem float64, replicas int) CostBreakdown {
	cpuUnitPrice, memUnitPrice := UnitPrices(cfg)
	replicaCount := float64(NormalizeReplicas(replicas))

	breakdown := CostBreakdown{
		CPUCost: (reqCPU / 1000.0) * cpuUnitPrice * replicaCount,
		MemCost: reqMem * memUnitPrice * replicaCount,
	}
	breakdown.TotalCost = breakdown.CPUCost + breakdown.MemCost
	return breakdown
}

func WithGatewayCost(base CostBreakdown, gatewayCost float64) CostBreakdown {
	base.GatewayCost = gatewayCost
	base.TotalCost = base.CPUCost + base.MemCost + base.GatewayCost
	return base
}

func (gcc *GatewayCostCalculator) CalculateHealthWaste(podCost, gwShareCost, errorRate5xx float64) float64 {
	totalCost := podCost + gwShareCost
	return totalCost * errorRate5xx
}

func (gcc *GatewayCostCalculator) CalculateHealthScore(errorRate5xx, wastedSpend, monthlyCost float64) float64 {
	score := 100.0
	score -= errorRate5xx * 100
	if monthlyCost > 0 {
		wastedRatio := wastedSpend / monthlyCost
		score -= wastedRatio * 50
	}
	return math.Max(0, score)
}

func (gcc *GatewayCostCalculator) CalculatePriority(fullUnitCost, rpsWeight float64) float64 {
	return fullUnitCost * (1.0 - rpsWeight)
}

func (gcc *GatewayCostCalculator) IsBlackHole(rpsWeight, podCost float64) bool {
	return rpsWeight < 0.01 && podCost > gcc.cfg.Governance.BlackHoleCostThreshold
}

func (gcc *GatewayCostCalculator) IsHighTrafficService(rpsWeight float64) bool {
	return rpsWeight > gcc.cfg.Governance.HighTrafficThreshold
}

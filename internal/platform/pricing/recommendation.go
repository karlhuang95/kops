package pricing

import (
	"math"

	"kops/pkg/config"
	"kops/pkg/model"
)

// Quantize rounds val up to the nearest multiple of step, clamped to min.
func Quantize(val float64, step int, min int) int {
	if step <= 0 {
		return min
	}
	if val < float64(min) {
		return min
	}
	return int(math.Ceil(val/float64(step)) * float64(step))
}

// RecommendCPU calculates the recommended CPU request in millicores.
// Cold-start detection uses cfg.Governance.P95LowThreshold.
func RecommendCPU(cfg *config.GlobalConfig, cpuP95, cpuRequest float64) int {
	if cpuRequest <= 0 || cpuP95 < cfg.Governance.P95LowThreshold*cpuRequest {
		return cfg.Governance.MinCPU
	}
	rawRecCPU := cpuP95 / cfg.Governance.TargetUtilization
	return Quantize(rawRecCPU, cfg.Governance.CPUStep, cfg.Governance.MinCPU)
}

// RecommendMemory calculates the recommended memory request in MiB.
// Uses memory_target_utilization to leave buffer (same logic as CPU recommendation).
func RecommendMemory(cfg *config.GlobalConfig, memMax float64) int {
	target := cfg.Governance.MemoryTargetUtilization
	if target <= 0 || target > 1 {
		target = 0.8
	}
	rawRecMem := memMax / target
	return Quantize(rawRecMem, cfg.Governance.MemoryStep, cfg.Governance.MinMemory)
}

// CalculateTotalTrafficRPS sums AvgRPS across all non-task services.
func CalculateTotalTrafficRPS(cfg *config.GlobalConfig, metrics []model.ServiceMetrics) float64 {
	total := 0.0
	for _, m := range metrics {
		if !IsTaskService(cfg, m.Deployment) {
			total += m.AvgRPS
		}
	}
	return total
}

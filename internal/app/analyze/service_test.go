package analyze

import (
	"testing"

	metricsdomain "kops/internal/domain/metrics"
	platformconfig "kops/internal/platform/config"
	advisorpkg "kops/pkg/advisor"
)

func TestBuildAdviceResultsSkipsEmptyMetrics(t *testing.T) {
	cfg := &platformconfig.GlobalConfig{
		Cost: platformconfig.CostSpec{
			Price:    1000,
			Count:    1,
			CPUCores: 10,
			MemoryGB: 20,
			ResourceWeight: platformconfig.ResourceWeight{
				CPU:    1,
				Memory: 0,
			},
		},
		Governance: platformconfig.GovSpec{
			TargetUtilization:     0.8,
			CPUStep:               50,
			MemoryStep:            128,
			MinCPU:                100,
			MinMemory:             128,
			P95LowThreshold:       0.2,
			ThrottleHighThreshold: 10,
			MemHighThreshold:      0.9,
			RPSDensityThreshold:   0.1,
		},
		GatewayCost: platformconfig.GatewaySpec{
			Price: 100,
			Count: 1,
		},
	}

	engine := advisorpkg.NewEngine(cfg)
	engine.SetTotalRPS(10)

	metrics := []metricsdomain.ServiceMetrics{
		{
			Namespace:   "prod",
			Deployment:  "api-service",
			CPURequest:  200,
			MemRequest:  256,
			CPUUsageMax: 120,
			CPUUsageAvg: 80,
			MemUsageMax: 200,
			AvgRPS:      10,
			Replicas:    1,
		},
		{
			Namespace:  "prod",
			Deployment: "empty-service",
		},
	}

	results := buildAdviceResults(engine, metrics)
	if len(results) != 1 {
		t.Fatalf("buildAdviceResults() len = %d, want 1", len(results))
	}
	if results[0].Deployment != "api-service" {
		t.Fatalf("buildAdviceResults()[0].Deployment = %q, want %q", results[0].Deployment, "api-service")
	}
}

func TestIdentifyResourceBlackHole(t *testing.T) {
	cfg := &platformconfig.GlobalConfig{
		Cost: platformconfig.CostSpec{
			Price:    1000,
			Count:    1,
			CPUCores: 10,
			MemoryGB: 20,
			ResourceWeight: platformconfig.ResourceWeight{
				CPU:    1,
				Memory: 0,
			},
		},
		Governance: platformconfig.GovSpec{
			BlackHoleCostThreshold: 50,
		},
	}

	tests := []struct {
		name      string
		metrics   metricsdomain.ServiceMetrics
		wantOK    bool
		wantWaste float64
	}{
		{
			name: "detects expensive underutilized service",
			metrics: metricsdomain.ServiceMetrics{
				Namespace:   "prod",
				Deployment:  "api-service",
				CPURequest:  1000,
				CPUUsageAvg: 50,
				Replicas:    1,
			},
			wantOK:    true,
			wantWaste: 95,
		},
		{
			name: "ignores low cost service",
			metrics: metricsdomain.ServiceMetrics{
				Namespace:   "prod",
				Deployment:  "small-service",
				CPURequest:  400,
				CPUUsageAvg: 10,
				Replicas:    1,
			},
			wantOK: false,
		},
		{
			name: "ignores service above utilization threshold",
			metrics: metricsdomain.ServiceMetrics{
				Namespace:   "prod",
				Deployment:  "busy-service",
				CPURequest:  1000,
				CPUUsageAvg: 150,
				AvgRPS:      1.0,
				Replicas:    1,
			},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := identifyResourceBlackHole(tt.metrics, cfg)
			if ok != tt.wantOK {
				t.Fatalf("identifyResourceBlackHole() ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.WasteAmount != tt.wantWaste {
				t.Fatalf("identifyResourceBlackHole() waste = %.2f, want %.2f", got.WasteAmount, tt.wantWaste)
			}
		})
	}
}

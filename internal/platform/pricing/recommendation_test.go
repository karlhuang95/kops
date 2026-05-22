package pricing

import (
	"testing"

	"kops/pkg/config"
	"kops/pkg/model"
)

func TestQuantize(t *testing.T) {
	tests := []struct {
		name string
		val  float64
		step int
		min  int
		want int
	}{
		{name: "exact step", val: 200, step: 50, min: 100, want: 200},
		{name: "rounds up", val: 220, step: 50, min: 100, want: 250},
		{name: "clamped to min", val: 30, step: 50, min: 100, want: 100},
		{name: "zero val", val: 0, step: 50, min: 100, want: 100},
		{name: "zero step", val: 200, step: 0, min: 100, want: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Quantize(tt.val, tt.step, tt.min)
			if got != tt.want {
				t.Fatalf("Quantize(%v, %d, %d) = %d, want %d", tt.val, tt.step, tt.min, got, tt.want)
			}
		})
	}
}

func TestRecommendCPU(t *testing.T) {
	cfg := &config.GlobalConfig{
		Governance: config.GovSpec{
			TargetUtilization: 0.8,
			CPUStep:           50,
			MinCPU:            100,
			P95LowThreshold:   0.2,
		},
	}

	// cold-start: usage below 20% of request
	if got := RecommendCPU(cfg, 50, 500); got != 100 {
		t.Fatalf("cold-start: RecommendCPU = %d, want 100", got)
	}

	// normal: usage above threshold
	if got := RecommendCPU(cfg, 400, 500); got != 500 {
		t.Fatalf("normal: RecommendCPU = %d, want 500", got)
	}

	// zero request returns min
	if got := RecommendCPU(cfg, 50, 0); got != 100 {
		t.Fatalf("zero request: RecommendCPU = %d, want 100", got)
	}
}

func TestRecommendMemory(t *testing.T) {
	cfg := &config.GlobalConfig{
		Governance: config.GovSpec{
			MemoryStep: 128,
			MinMemory:  128,
		},
	}

	if got := RecommendMemory(cfg, 200); got != 256 {
		t.Fatalf("RecommendMemory(200) = %d, want 256", got)
	}

	if got := RecommendMemory(cfg, 50); got != 128 {
		t.Fatalf("RecommendMemory(50) = %d, want 128", got)
	}
}

func TestCalculateTotalTrafficRPS(t *testing.T) {
	cfg := &config.GlobalConfig{
		Governance: config.GovSpec{
			TaskServicePatterns: []string{"cron", "consumer", "job", "worker"},
		},
	}
	metrics := []model.ServiceMetrics{
		{Deployment: "api-gateway", AvgRPS: 120},
		{Deployment: "order-worker", AvgRPS: 80},
		{Deployment: "billing-cron", AvgRPS: 50},
		{Deployment: "frontend", AvgRPS: 30},
	}

	got := CalculateTotalTrafficRPS(cfg, metrics)
	if got != 150.0 {
		t.Fatalf("CalculateTotalTrafficRPS() = %.2f, want 150.0", got)
	}
}

package algorithm

import (
	"kops/pkg/config"
	"kops/pkg/model"
	"testing"
)

func TestCalculateGatewayTotalCost(t *testing.T) {
	cfg := &config.GlobalConfig{
		GatewayCost: config.GatewaySpec{
			Price: 478.47,
			Count: 2,
		},
	}

	calc := NewGatewayCostCalculator(cfg)
	totalCost := calc.CalculateGatewayTotalCost()

	expected := 478.47 * 2
	if totalCost != expected {
		t.Errorf("Expected gateway total cost %.2f, got %.2f", expected, totalCost)
	}
}

func TestCalculateKopsMetrics(t *testing.T) {
	cfg := &config.GlobalConfig{
		Cost: config.CostSpec{
			Price:    1197.42,
			Count:    4,
			CPUCores: 16,
			MemoryGB: 64,
			ResourceWeight: config.ResourceWeight{
				CPU:    1.0,
				Memory: 0.0,
			},
		},
		Governance: config.GovSpec{
			TaskServicePatterns: []string{"cron", "consumer", "job", "worker"},
		},
	}

	calc := NewGatewayCostCalculator(cfg)

	// 测试服务 A (Traffic-based - Web服务)
	serviceA := &model.ServiceMetrics{
		Deployment: "api-service",
		AvgRPS:     100.0,
		CPURequest: 500.0, // 500m CPU
		MemRequest: 512.0, // 512MiB
		Replicas:   2,
	}

	// 测试服务 B (Traffic-based - Web服务)
	serviceB := &model.ServiceMetrics{
		Deployment: "web-frontend",
		AvgRPS:     10.0,
		CPURequest: 100.0, // 100m CPU
		MemRequest: 128.0, // 128MiB
		Replicas:   1,
	}

	// 测试服务 C (Task-based - Cron服务，不走Traefik)
	serviceC := &model.ServiceMetrics{
		Deployment: "data-sync-cron",
		AvgRPS:     0.0,
		CPURequest: 500.0,
		MemRequest: 512.0,
		Replicas:   1,
	}

	// 新方案：仅统计 Traffic-based 服务的 RPS
	totalTrafficRPS := 100.0 + 10.0 // serviceA + serviceB
	gwTotalCost := 956.94           // 478.47 * 2

	// 计算服务 A 的指标
	calc.CalculateKopsMetrics(serviceA, totalTrafficRPS, gwTotalCost)

	// 验证服务 A 是 Traffic-based
	if !serviceA.IsGatewayUser {
		t.Error("Service A should be a Gateway user (Traffic-based)")
	}

	// 验证流量权重
	expectedWeightA := 100.0 / 110.0
	if serviceA.RpsWeight != expectedWeightA {
		t.Errorf("Service A: Expected RpsWeight %.4f, got %.4f", expectedWeightA, serviceA.RpsWeight)
	}

	// 验证 Gateway 分摊成本
	expectedGwCostA := expectedWeightA * gwTotalCost
	if serviceA.GwShareCost != expectedGwCostA {
		t.Errorf("Service A: Expected GwShareCost %.2f, got %.2f", expectedGwCostA, serviceA.GwShareCost)
	}

	// 计算服务 B 的指标
	calc.CalculateKopsMetrics(serviceB, totalTrafficRPS, gwTotalCost)

	// 验证服务 B 是 Traffic-based
	if !serviceB.IsGatewayUser {
		t.Error("Service B should be a Gateway user (Traffic-based)")
	}

	// 验证流量权重
	expectedWeightB := 10.0 / 110.0
	if serviceB.RpsWeight != expectedWeightB {
		t.Errorf("Service B: Expected RpsWeight %.4f, got %.4f", expectedWeightB, serviceB.RpsWeight)
	}

	// 验证 Gateway 分摊成本
	expectedGwCostB := expectedWeightB * gwTotalCost
	if serviceB.GwShareCost != expectedGwCostB {
		t.Errorf("Service B: Expected GwShareCost %.2f, got %.2f", expectedGwCostB, serviceB.GwShareCost)
	}

	// 计算服务 C (Cron) 的指标
	calc.CalculateKopsMetrics(serviceC, totalTrafficRPS, gwTotalCost)

	// 验证服务 C 不是 Traffic-based
	if serviceC.IsGatewayUser {
		t.Error("Service C (Cron) should NOT be a Gateway user (Task-based)")
	}

	// 验证 Cron 服务不分摊 Gateway 成本
	if serviceC.RpsWeight != 0 {
		t.Errorf("Service C (Cron): Expected RpsWeight 0, got %.4f", serviceC.RpsWeight)
	}

	if serviceC.GwShareCost != 0 {
		t.Errorf("Service C (Cron): Expected GwShareCost 0, got %.2f", serviceC.GwShareCost)
	}
}

func TestIsTaskService(t *testing.T) {
	cfg := &config.GlobalConfig{
		Governance: config.GovSpec{
			TaskServicePatterns: []string{"cron", "consumer", "job", "worker"},
		},
	}
	tests := []struct {
		name       string
		deployment string
		expected   bool
	}{
		{name: "cron", deployment: "daily-cleanup-cron", expected: true},
		{name: "consumer", deployment: "payment-consumer", expected: true},
		{name: "worker", deployment: "image-worker", expected: true},
		{name: "job", deployment: "sync-job-runner", expected: true},
		{name: "web", deployment: "api-gateway", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTaskService(cfg, tt.deployment); got != tt.expected {
				t.Fatalf("IsTaskService(%q) = %v, want %v", tt.deployment, got, tt.expected)
			}
		})
	}
}

func TestCalculateKopsMetricsMemoryPriceDoesNotMultiplyByNodeCount(t *testing.T) {
	cfg := &config.GlobalConfig{
		Cost: config.CostSpec{
			Price:    1000,
			Count:    4,
			CPUCores: 10,
			MemoryGB: 10,
			ResourceWeight: config.ResourceWeight{
				CPU:    0,
				Memory: 1,
			},
		},
	}

	svc := &model.ServiceMetrics{
		Deployment: "api-service",
		MemRequest: 1024,
		Replicas:   1,
	}

	calc := NewGatewayCostCalculator(cfg)
	calc.CalculateKopsMetrics(svc, 0, 0)

	expected := 100.0
	if svc.PodCost != expected {
		t.Fatalf("expected pod cost %.2f, got %.2f", expected, svc.PodCost)
	}
}

func TestCalculatePriority(t *testing.T) {
	cfg := &config.GlobalConfig{}
	calc := NewGatewayCostCalculator(cfg)

	// 高单位成本 + 低流量权重 = 高优先级
	priority1 := calc.CalculatePriority(0.1, 0.01) // 优先级应该较高

	// 低单位成本 + 高流量权重 = 低优先级
	priority2 := calc.CalculatePriority(0.01, 0.5) // 优先级应该较低

	if priority1 <= priority2 {
		t.Errorf("Expected priority1 (%.4f) > priority2 (%.4f)", priority1, priority2)
	}
}

func TestIsBlackHole(t *testing.T) {
	cfg := &config.GlobalConfig{
		Governance: config.GovSpec{
			BlackHoleCostThreshold: 100.0,
		},
	}
	calc := NewGatewayCostCalculator(cfg)

	// 资源黑洞：低权重 + 高成本
	isBlackHole1 := calc.IsBlackHole(0.005, 150.0)
	if !isBlackHole1 {
		t.Error("Expected black hole detection for low weight and high cost")
	}

	// 非黑洞：低权重 + 低成本
	isBlackHole2 := calc.IsBlackHole(0.005, 50.0)
	if isBlackHole2 {
		t.Error("Expected non-black hole for low weight and low cost")
	}

	// 非黑洞：高权重 + 高成本
	isBlackHole3 := calc.IsBlackHole(0.5, 150.0)
	if isBlackHole3 {
		t.Error("Expected non-black hole for high weight and high cost")
	}
}

func TestIsHighTrafficService(t *testing.T) {
	cfg := &config.GlobalConfig{
		Governance: config.GovSpec{
			HighTrafficThreshold: 0.3,
		},
	}
	calc := NewGatewayCostCalculator(cfg)

	// 核心流量服务
	isHighTraffic1 := calc.IsHighTrafficService(0.5)
	if !isHighTraffic1 {
		t.Error("Expected high traffic service for weight > 0.3")
	}

	// 非核心流量服务
	isHighTraffic2 := calc.IsHighTrafficService(0.1)
	if isHighTraffic2 {
		t.Error("Expected non-high traffic service for weight < 0.3")
	}
}

func TestCalculateHealthWaste(t *testing.T) {
	cfg := &config.GlobalConfig{}
	calc := NewGatewayCostCalculator(cfg)

	podCost := 100.0
	gwShareCost := 50.0
	errorRate := 0.1 // 10% 错误率

	wastedSpend := calc.CalculateHealthWaste(podCost, gwShareCost, errorRate)
	expected := (100.0 + 50.0) * 0.1

	if wastedSpend != expected {
		t.Errorf("Expected wasted spend %.2f, got %.2f", expected, wastedSpend)
	}
}

func TestCalculateHealthScore(t *testing.T) {
	cfg := &config.GlobalConfig{}
	calc := NewGatewayCostCalculator(cfg)

	// 高错误率 + 高延迟 + 多重启 → 分数较低
	score1 := calc.CalculateHealthScore(0.2, 0.1, 3.0, 5, 0.9)

	// 低错误率 + 低延迟 + 无重启 → 分数较高
	score2 := calc.CalculateHealthScore(0.01, 0.0, 0.5, 0, 0.5)

	if score1 >= score2 {
		t.Errorf("Expected score1 (%.2f) < score2 (%.2f)", score1, score2)
	}

	// 分数应该在 0-100 之间
	if score1 < 0 || score1 > 100 {
		t.Errorf("Score1 should be between 0 and 100, got %.2f", score1)
	}
	if score2 < 0 || score2 > 100 {
		t.Errorf("Score2 should be between 0 and 100, got %.2f", score2)
	}
}

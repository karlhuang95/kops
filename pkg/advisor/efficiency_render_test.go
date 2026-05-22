package advisor

import (
	"strings"
	"testing"

	"kops/pkg/config"
	"kops/pkg/model"
)

func TestRenderEfficiencyReportTableIncludesSections(t *testing.T) {
	engine := NewEfficiencyEngine(&config.GlobalConfig{})
	results := []model.EfficiencyResult{
		{
			ServiceName:        "svc-a",
			CurrentCPU:         500,
			RecCPU:             300,
			TrafficDensity:     120.5,
			TrafficDensityRank: "B (低效)",
			CurrentCost:        100,
			RecCost:            60,
			ActualCost:         40,
			WasteAmount:        60,
			MonthlySaving:      40,
			WasteRatio:         0.6,
			Action:             "分步缩容",
		},
	}
	blackHoles := []model.ResourceBlackHole{
		{
			ServiceName: "svc-a",
			Namespace:   "prod",
			CurrentCost: 100,
			ActualCost:  10,
			WasteRatio:  0.9,
			WasteAmount: 90,
		},
	}

	output := engine.RenderEfficiencyReport(results, blackHoles, "table")
	checks := []string{
		"流量效能与资源黑洞分析报告",
		"发现资源黑洞: 1",
		"Top 5 资源黑洞",
		"svc-a",
		"分步缩容",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Fatalf("RenderEfficiencyReport() missing %q in output:\n%s", check, output)
		}
	}
}

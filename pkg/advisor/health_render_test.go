package advisor

import (
	"strings"
	"testing"

	"kops/pkg/model"
)

func TestRenderHealthReportTableIncludesSummary(t *testing.T) {
	engine := NewHealthEngine(nil)
	results := []model.HealthStatus{
		{
			ServiceName:      "svc-a",
			Namespace:        "prod",
			HealthCode:       "Critical",
			StatusIcon:       "🔴",
			RPS:              12,
			Error5xxRate:     0.12,
			P99Latency:       1.5,
			InvalidSpend:     50,
			WastedSpendTotal: 60,
			Diagnosis:        "后端异常",
			Action:           "立即排查",
		},
	}

	output := engine.RenderHealthReport(results, "table")
	checks := []string{
		"服务健康检查报告",
		"服务总数:   1",
		"全链路损耗: ￥60.00/月",
		"prod/svc-a",
		"立即排查",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Fatalf("RenderHealthReport() missing %q in output:\n%s", check, output)
		}
	}
}

package serve

import (
	"fmt"
	"html/template"
	"strings"

	healthdomain "kops/internal/domain/health"
)

var funcMap = template.FuncMap{
	"formatFloat":      func(v float64) string { return fmt.Sprintf("%.2f", v) },
	"formatInt":        func(v int) string { return fmt.Sprintf("%d", v) },
	"formatPercent":    func(v float64) string { return fmt.Sprintf("%.2f%%", v*100) },
	"add":              func(a, b int) int { return a + b },
	"mul":              func(a, b float64) float64 { return a * b },
	"riskBadgeClass":   riskBadgeClass,
	"densityRankClass": densityRankClass,
	"healthCodeClass":  healthCodeClass,
	"healthRowClass":   healthRowClass,
	"cpuDiffPercent":   cpuDiffPercent,
	"avgHealthScore":   avgHealthScore,
}

func cpuDiffPercent(oldCPU, newCPU int) float64 {
	if oldCPU <= 0 {
		return 0
	}
	diff := float64(oldCPU-newCPU) / float64(oldCPU) * 100
	if diff < 0 {
		diff = -diff
	}
	if diff > 100 {
		diff = 100
	}
	return diff
}

func avgHealthScore(statuses interface{}) string {
	type hasHealthScore interface {
		GetHealthScore() float64
	}
	// Use type assertion for slice of health statuses
	switch s := statuses.(type) {
	case []healthdomain.HealthStatus:
		if len(s) == 0 {
			return "0.0"
		}
		var total float64
		var count int
		for _, st := range s {
			if st.HealthCode != "Idle" {
				total += st.HealthScore
				count++
			}
		}
		if count == 0 {
			return "0.0"
		}
		return fmt.Sprintf("%.0f", total/float64(count))
	default:
		return "0.0"
	}
}

func riskBadgeClass(level string) string {
	switch level {
	case "高":
		return "bg-danger"
	case "中":
		return "bg-warning text-dark"
	case "低":
		return "bg-success"
	default:
		return "bg-secondary"
	}
}

func densityRankClass(rank string) string {
	switch {
	case strings.HasPrefix(rank, "S"):
		return "bg-success"
	case strings.HasPrefix(rank, "A"):
		return "bg-info"
	case strings.HasPrefix(rank, "B"):
		return "bg-warning text-dark"
	case strings.HasPrefix(rank, "C"):
		return "bg-secondary"
	default:
		return "bg-secondary"
	}
}

func healthCodeClass(code string) string {
	switch code {
	case "Critical":
		return "bg-danger"
	case "Warning":
		return "bg-warning text-dark"
	case "Healthy":
		return "bg-success"
	case "Idle":
		return "bg-secondary"
	default:
		return "bg-secondary"
	}
}

func healthRowClass(code string) string {
	switch code {
	case "Critical":
		return "table-danger"
	case "Warning":
		return "table-warning"
	default:
		return ""
	}
}

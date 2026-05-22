package serve

import (
	"fmt"
	"html/template"
	"strings"
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

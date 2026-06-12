package serve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	appanalyze "kops/internal/app/analyze"
)

// AlertNotifier sends alerts based on analysis results.
type AlertNotifier struct {
	webhookURL string
	client     *http.Client
}

// NewAlertNotifier creates a new notifier with the given webhook URL.
func NewAlertNotifier(webhookURL string) *AlertNotifier {
	return &AlertNotifier{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

// CheckAndAlert evaluates analysis data against thresholds and sends alerts if needed.
func (an *AlertNotifier) CheckAndAlert(data *appanalyze.AnalysisData) {
	if an.webhookURL == "" {
		return
	}

	var alerts []string

	if data.CriticalCount > 0 {
		alerts = append(alerts, fmt.Sprintf("🔴 %d services in CRITICAL health state", data.CriticalCount))
	}
	if data.WarningCount > 5 {
		alerts = append(alerts, fmt.Sprintf("🟡 %d services in WARNING health state (threshold: 5)", data.WarningCount))
	}
	if len(data.BlackHoles) > 0 {
		totalWaste := 0.0
		for _, bh := range data.BlackHoles {
			totalWaste += bh.WasteAmount
		}
		alerts = append(alerts, fmt.Sprintf("🕳️ %d resource black holes detected, wasting $%.2f/month", len(data.BlackHoles), totalWaste))
	}
	if data.TotalMonthlySaving > 500 {
		alerts = append(alerts, fmt.Sprintf("💰 Potential monthly savings: $%.2f", data.TotalMonthlySaving))
	}

	if len(alerts) == 0 {
		return
	}

	payload := map[string]interface{}{
		"text":        fmt.Sprintf("kops Alert — %s\n\n%s", time.Now().Format(time.RFC3339), joinStrings(alerts, "\n")),
		"alerts":      alerts,
		"timestamp":   time.Now().Format(time.RFC3339),
		"serviceCount": len(data.AdvisorResults),
		"criticalCount": data.CriticalCount,
		"warningCount":  data.WarningCount,
	}

	b, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to marshal alert", "error", err)
		return
	}

	resp, err := an.client.Post(an.webhookURL, "application/json", bytes.NewReader(b))
	if err != nil {
		slog.Error("failed to send alert", "webhook", an.webhookURL, "error", err)
		return
	}
	defer resp.Body.Close()

	slog.Info("alert sent", "webhook", an.webhookURL, "count", len(alerts), "status", resp.StatusCode)
}

func joinStrings(items []string, sep string) string {
	if len(items) == 0 {
		return ""
	}
	result := items[0]
	for i := 1; i < len(items); i++ {
		result += sep + items[i]
	}
	return result
}

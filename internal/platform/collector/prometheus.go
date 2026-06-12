package collector

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"kops/pkg/config"
	"kops/pkg/model"
)

type PromCollector struct {
	cfg     *config.GlobalConfig
	Address string
	Timeout time.Duration
}

func NewCollector(cfg *config.GlobalConfig) *PromCollector {
	timeout := 30 * time.Second
	if d, err := time.ParseDuration(cfg.Prometheus.Timeout); err == nil && d > 0 {
		timeout = d
	}
	return &PromCollector{
		cfg:     cfg,
		Address: strings.TrimRight(cfg.Prometheus.Address, "/"),
		Timeout: timeout,
	}
}

func (pc *PromCollector) CollectAll(namespaces []string) ([]model.ServiceMetrics, error) {
	var results []model.ServiceMetrics
	var mu sync.Mutex
	var wg sync.WaitGroup

	deploys, err := pc.discoverDeployments(namespaces)
	if err != nil {
		return nil, err
	}

	semaphore := make(chan struct{}, pc.cfg.Governance.MaxCollectorConcurrency)
	for _, d := range deploys {
		wg.Add(1)
		go func(ns, name string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			metrics := pc.fetchSingleService(ns, name)

			mu.Lock()
			results = append(results, metrics)
			mu.Unlock()
		}(d.Namespace, d.Name)
	}

	wg.Wait()
	return results, nil
}

func (pc *PromCollector) discoverDeployments(namespaces []string) ([]struct{ Namespace, Name string }, error) {
	var list []struct{ Namespace, Name string }
	for _, ns := range namespaces {
		u := fmt.Sprintf("%s/api/v1/query?query=%s", pc.Address, url.QueryEscape(fmt.Sprintf(`kube_deployment_created{namespace="%s"}`, ns)))
		data, err := pc.doQuery(u)
		if err != nil {
			continue
		}
		for _, r := range data {
			if deployName, ok := r.Metric["deployment"]; ok && deployName != "" {
				list = append(list, struct{ Namespace, Name string }{
					Namespace: ns,
					Name:      deployName,
				})
			}
		}
	}
	return list, nil
}

func (pc *PromCollector) fetchSingleService(ns, deploy string) model.ServiceMetrics {
	m := model.ServiceMetrics{
		Namespace:  ns,
		Deployment: deploy,
		AppGroup:   extractAppGroup(deploy),
	}

	m.CPURequest = pc.queryScalar(fmt.Sprintf(`max(kube_pod_container_resource_requests{namespace="%s", pod=~"^%s-.*", pod!~".*-consumer-.*|.*-cron-.*|.*-job-.*|.*-worker-.*", resource="cpu"}) * 1000`, ns, deploy))
	m.CPULimit = pc.queryScalar(fmt.Sprintf(`max(kube_pod_container_resource_limits{namespace="%s", pod=~"^%s-.*", pod!~".*-consumer-.*|.*-cron-.*|.*-job-.*|.*-worker-.*", resource="cpu"}) * 1000`, ns, deploy))
	m.MemRequest = pc.queryScalar(fmt.Sprintf(`max(kube_pod_container_resource_requests{namespace="%s", pod=~"^%s-.*", pod!~".*-consumer-.*|.*-cron-.*|.*-job-.*|.*-worker-.*", resource="memory"}) / 1024 / 1024`, ns, deploy))
	m.MemLimit = pc.queryScalar(fmt.Sprintf(`max(kube_pod_container_resource_limits{namespace="%s", pod=~"^%s-.*", pod!~".*-consumer-.*|.*-cron-.*|.*-job-.*|.*-worker-.*", resource="memory"}) / 1024 / 1024`, ns, deploy))

	replicas := pc.queryScalar(fmt.Sprintf(`kube_deployment_spec_replicas{namespace="%s", deployment="%s"}`, ns, deploy))
	m.Replicas = int(replicas)
	if m.Replicas == 0 {
		m.Replicas = 1
	}

	m.CPUUsageMax = pc.queryScalar(fmt.Sprintf(`quantile_over_time(0.95, sum(rate(container_cpu_usage_seconds_total{namespace="%s", container!="", pod=~"^%s-.*", pod!~".*-consumer-.*|.*-cron-.*|.*-job-.*|.*-worker-.*"}[5m]))[7d:5m]) * 1000`, ns, deploy))
	m.CPUUsageAvg = pc.queryScalar(fmt.Sprintf(`avg_over_time(sum(rate(container_cpu_usage_seconds_total{namespace="%s", container!="", pod=~"^%s-.*", pod!~".*-consumer-.*|.*-cron-.*|.*-job-.*|.*-worker-.*"}[5m]))[7d:1h]) * 1000`, ns, deploy))
	m.MemUsageMax = pc.queryScalar(fmt.Sprintf(`max_over_time(sum(container_memory_working_set_bytes{namespace="%s", container!="", pod=~"^%s-.*", pod!~".*-consumer-.*|.*-cron-.*|.*-job-.*|.*-worker-.*"})[7d:5m]) / 1024 / 1024`, ns, deploy))

	m.ThrottleSecond = pc.queryScalar(fmt.Sprintf(`sum(increase(container_cpu_cfs_throttled_seconds_total{namespace="%s", pod=~"^%s-.*", pod!~".*-consumer-.*|.*-cron-.*|.*-job-.*|.*-worker-.*"}[24h]))`, ns, deploy))
	m.AvgRPS = pc.queryScalar(fmt.Sprintf(`avg_over_time(sum(rate(traefik_service_requests_total{exported_service=~"%s-%s-.*"}[5m]))[7d:1h])`, ns, deploy))

	return m
}

// CollectSingleService fetches metrics for a single deployment (public wrapper).
// Falls back to fuzzy matching if exact deployment name not found.
func (pc *PromCollector) CollectSingleService(ns, deploy string) model.ServiceMetrics {
	// Try exact deployment name first
	m := pc.fetchSingleService(ns, deploy)
	if m.CPURequest > 0 {
		return m
	}
	// Try with -prod suffix (common naming convention)
	m2 := pc.fetchSingleService(ns, deploy+"-prod")
	if m2.CPURequest > 0 {
		return m2
	}
	// Return whatever we got from regex matching
	return m
}

func extractAppGroup(deploy string) string {
	parts := strings.Split(deploy, "-")
	if len(parts) > 0 {
		return parts[0]
	}
	return deploy
}

func (pc *PromCollector) queryScalar(query string) float64 {
	u := fmt.Sprintf("%s/api/v1/query?query=%s", pc.Address, url.QueryEscape(query))
	res, err := pc.doQuery(u)
	if err != nil || len(res) == 0 {
		return 0
	}
	val, _ := strconv.ParseFloat(res[0].Value, 64)
	return val
}

func (pc *PromCollector) doQuery(apiURL string) ([]promResult, error) {
	client := &http.Client{Timeout: pc.Timeout}
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiRes struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  []interface{}     `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiRes); err != nil {
		return nil, err
	}

	var out []promResult
	for _, r := range apiRes.Data.Result {
		if len(r.Value) < 2 {
			continue
		}
		out = append(out, promResult{
			Metric: r.Metric,
			Value:  fmt.Sprintf("%v", r.Value[1]),
		})
	}
	return out, nil
}

// TimeSeriesPoint is a single timestamped data point from a range query.
type TimeSeriesPoint struct {
	Timestamp int64
	Value     float64
}

// QueryRange executes a Prometheus range query and returns time-series data.
func (pc *PromCollector) QueryRange(queryStr string, start, end time.Time, step time.Duration) ([]TimeSeriesPoint, error) {
	u := fmt.Sprintf("%s/api/v1/query_range?query=%s&start=%d&end=%d&step=%d",
		pc.Address, url.QueryEscape(queryStr), start.Unix(), end.Unix(), int(step.Seconds()))

	client := &http.Client{Timeout: pc.Timeout}
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiRes struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Values [][]interface{}    `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiRes); err != nil {
		return nil, err
	}

	var points []TimeSeriesPoint
	for _, r := range apiRes.Data.Result {
		for _, v := range r.Values {
			if len(v) < 2 {
				continue
			}
			ts := parseNumeric(v[0])
			val := parseNumeric(v[1])
			points = append(points, TimeSeriesPoint{Timestamp: int64(ts), Value: val})
		}
	}
	return points, nil
}

// parseNumeric converts a Prometheus JSON number (which may be float64 or string) to float64.
func parseNumeric(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	default:
		s := fmt.Sprintf("%v", v)
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
}

type promResult struct {
	Metric map[string]string
	Value  string
}

package collector

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"kops/pkg/algorithm"
	"kops/pkg/config"
	"kops/pkg/model"
)

type TraefikCollector struct {
	prom *PromCollector
	cfg  *config.GlobalConfig
}

func NewTraefikCollector(cfg *config.GlobalConfig) *TraefikCollector {
	return &TraefikCollector{
		prom: NewCollector(cfg),
		cfg:  cfg,
	}
}

func (tc *TraefikCollector) buildQueryURL(query string) string {
	return fmt.Sprintf("%s/api/v1/query?query=%s", tc.prom.Address, url.QueryEscape(query))
}

// parseServiceName extracts the service name from a Traefik exported_service value.
// The namespace is already known (from the Traefik query), so we strip it as a prefix
// to avoid ambiguity from dashes in names.
func parseServiceName(traefikService, namespace string) string {
	cleanName := strings.TrimSuffix(traefikService, "@kubernetescrd")

	// Strip known namespace prefix: "{namespace}-"
	prefix := namespace + "-"
	rest, ok := strings.CutPrefix(cleanName, prefix)
	if !ok {
		return cleanName
	}

	// rest is "{service}-{port}-{hash}" — strip the last two dash-separated segments
	for i := 0; i < 2; i++ {
		idx := strings.LastIndex(rest, "-")
		if idx < 0 {
			return rest
		}
		rest = rest[:idx]
	}

	return rest
}

func (tc *TraefikCollector) CollectHealthMetrics(servicePattern string, duration time.Duration, namespace string) (model.HealthMetrics, error) {
	serviceName := parseServiceName(servicePattern, namespace)

	metrics := model.HealthMetrics{
		ServiceName: serviceName,
		Namespace:   namespace,
	}

	rpsQuery := fmt.Sprintf(`sum(irate(traefik_service_requests_total{exported_service=~"%s"}[%s]))`,
		servicePattern, duration.String())
	metrics.RPS = tc.prom.queryScalar(rpsQuery)

	if metrics.RPS < 0.001 {
		return metrics, nil
	}

	error5xxQuery := fmt.Sprintf(`sum(irate(traefik_service_requests_total{exported_service=~"%s", code=~"5.."}[%s])) / sum(irate(traefik_service_requests_total{exported_service=~"%s"}[%s]))`,
		servicePattern, duration.String(), servicePattern, duration.String())
	metrics.Error5xxRate = tc.prom.queryScalar(error5xxQuery)

	error4xxQuery := fmt.Sprintf(`sum(irate(traefik_service_requests_total{exported_service=~"%s", code=~"4.."}[%s])) / sum(irate(traefik_service_requests_total{exported_service=~"%s"}[%s]))`,
		servicePattern, duration.String(), servicePattern, duration.String())
	metrics.Error4xxRate = tc.prom.queryScalar(error4xxQuery)

	p99Query := fmt.Sprintf(`histogram_quantile(0.99, sum(irate(traefik_service_request_duration_seconds_bucket{exported_service=~"%s"}[%s])) by (le))`,
		servicePattern, duration.String())
	metrics.P99Latency = tc.prom.queryScalar(p99Query)

	// 采集 CPU 利用率（用于容量风险检测）
	cpuUtilQuery := fmt.Sprintf(`avg(rate(container_cpu_usage_seconds_total{namespace="%s", pod=~"%s-.*", container!=""}[5m])) / avg(kube_pod_container_resource_requests{namespace="%s", pod=~"%s-.*", container!="", resource="cpu"})`,
		namespace, serviceName, namespace, serviceName)
	metrics.CPUUtilization = tc.prom.queryScalar(cpuUtilQuery)

	// 采集 Pod 重启次数（用于稳定性检测）
	restartQuery := fmt.Sprintf(`sum(increase(kube_pod_container_status_restarts_total{namespace="%s", pod=~"%s-.*"}[24h]))`,
		namespace, serviceName)
	metrics.RestartCount = int(tc.prom.queryScalar(restartQuery))

	return metrics, nil
}

func (tc *TraefikCollector) CollectServiceList(namespace string) ([]string, error) {
	query := fmt.Sprintf(`count by (exported_service) (traefik_service_requests_total{exported_service=~"%s-.*"})`, namespace)
	apiURL := tc.buildQueryURL(query)

	results, err := tc.prom.doQuery(apiURL)
	if err != nil {
		return nil, err
	}

	var services []string
	for _, result := range results {
		if exportedService, ok := result.Metric["exported_service"]; ok {
			services = append(services, exportedService)
		}
	}

	return services, nil
}

func (tc *TraefikCollector) GetServiceMonthlyCost(deployment string, namespace string) float64 {
	prom := NewCollector(tc.cfg)
	deployment = parseServiceName(deployment, namespace)

	cpuRequest := prom.queryScalar(fmt.Sprintf(`max(kube_pod_container_resource_requests{namespace="%s", pod=~"%s-.*", resource="cpu"}) * 1000`, namespace, deployment))
	memRequest := prom.queryScalar(fmt.Sprintf(`max(kube_pod_container_resource_requests{namespace="%s", pod=~"%s-.*", resource="memory"}) / 1024 / 1024`, namespace, deployment))

	replicasQuery := fmt.Sprintf(`kube_deployment_spec_replicas{namespace="%s", deployment="%s"}`, namespace, deployment)
	replicas := int(prom.queryScalar(replicasQuery))

	return algorithm.PodMonthlyCost(tc.cfg, cpuRequest, memRequest, replicas).TotalCost
}

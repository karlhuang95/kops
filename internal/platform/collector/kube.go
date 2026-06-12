package collector

import (
	"fmt"
	"net/url"
	"strconv"
)

// NodeMetrics holds resource allocation for a single K8s node.
type NodeMetrics struct {
	NodeName       string  `json:"nodeName"`
	AllocatableCPU float64 `json:"allocatableCPU"`
	AllocatableMem float64 `json:"allocatableMem"`
	RequestedCPU   float64 `json:"requestedCPU"`
	RequestedMem   float64 `json:"requestedMem"`
	PodCount       int     `json:"podCount"`
	CPUUtilization float64 `json:"cpuUtilization"`
	MemUtilization float64 `json:"memUtilization"`
}

// ClusterMetrics holds aggregated cluster-wide resource stats.
type ClusterMetrics struct {
	TotalNodes          int           `json:"totalNodes"`
	TotalAllocatableCPU float64       `json:"totalAllocatableCPU"`
	TotalAllocatableMem float64       `json:"totalAllocatableMem"`
	TotalRequestedCPU   float64       `json:"totalRequestedCPU"`
	TotalRequestedMem   float64       `json:"totalRequestedMem"`
	AvgCPUUtilization   float64       `json:"avgCPUUtilization"`
	AvgMemUtilization   float64       `json:"avgMemUtilization"`
	FragmentedNodes     []string      `json:"fragmentedNodes"`
	HotNodes            []string      `json:"hotNodes"`
	Nodes               []NodeMetrics `json:"nodes"`
}

// NodeScalingResult holds node scaling recommendations.
type NodeScalingResult struct {
	CurrentNodes        int      `json:"currentNodes"`
	RecommendedNodes    int      `json:"recommendedNodes"`
	CanRemoveNodes      bool     `json:"canRemoveNodes"`
	NodesToRemove       int      `json:"nodesToRemove"`
	NeedMoreNodes       bool     `json:"needMoreNodes"`
	NodesToAdd          int      `json:"nodesToAdd"`
	AvgCPUUtilization   float64  `json:"avgCPUUtilization"`
	AvgMemUtilization   float64  `json:"avgMemUtilization"`
	TotalAllocatableCPU float64  `json:"totalAllocatableCPU"`
	TotalRequestedCPU   float64  `json:"totalRequestedCPU"`
	FragmentedNodes     []string `json:"fragmentedNodes"`
	HotNodes            []string `json:"hotNodes"`
	MonthlySavings      float64  `json:"monthlySavings"`
}

// LabelCostEntry holds cost attribution for one label value.
type LabelCostEntry struct {
	LabelKey    string   `json:"labelKey"`
	LabelValue  string   `json:"labelValue"`
	ServiceCount int     `json:"serviceCount"`
	PodCost     float64  `json:"podCost"`
	GatewayCost float64  `json:"gatewayCost"`
	TotalCost   float64  `json:"totalCost"`
	Services    []string `json:"services"`
}

// CollectClusterMetrics collects node-level resource metrics from Prometheus.
func (pc *PromCollector) CollectClusterMetrics() (*ClusterMetrics, error) {
	cm := &ClusterMetrics{
		FragmentedNodes: make([]string, 0),
		HotNodes:        make([]string, 0),
		Nodes:           make([]NodeMetrics, 0),
	}

	nodes, err := pc.discoverNodes()
	if err != nil || len(nodes) == 0 {
		return cm, nil
	}
	cm.TotalNodes = len(nodes)

	for _, node := range nodes {
		nm := NodeMetrics{NodeName: node}

		nm.AllocatableCPU = pc.queryScalar(fmt.Sprintf(
			`kube_node_status_allocatable{node="%s", resource="cpu"} * 1000`, node))
		nm.AllocatableMem = pc.queryScalar(fmt.Sprintf(
			`kube_node_status_allocatable{node="%s", resource="memory"} / 1024 / 1024`, node))
		nm.RequestedCPU = pc.queryScalar(fmt.Sprintf(
			`sum(kube_pod_container_resource_requests{node="%s", resource="cpu"}) * 1000`, node))
		nm.RequestedMem = pc.queryScalar(fmt.Sprintf(
			`sum(kube_pod_container_resource_requests{node="%s", resource="memory"}) / 1024 / 1024`, node))
		nm.PodCount = int(pc.queryScalar(fmt.Sprintf(
			`count(kube_pod_info{node="%s"})`, node)))

		if nm.AllocatableCPU > 0 {
			nm.CPUUtilization = nm.RequestedCPU / nm.AllocatableCPU
		}
		if nm.AllocatableMem > 0 {
			nm.MemUtilization = nm.RequestedMem / nm.AllocatableMem
		}

		cm.TotalAllocatableCPU += nm.AllocatableCPU
		cm.TotalAllocatableMem += nm.AllocatableMem
		cm.TotalRequestedCPU += nm.RequestedCPU
		cm.TotalRequestedMem += nm.RequestedMem
		cm.Nodes = append(cm.Nodes, nm)
	}

	if cm.TotalAllocatableCPU > 0 {
		cm.AvgCPUUtilization = cm.TotalRequestedCPU / cm.TotalAllocatableCPU
	}
	if cm.TotalAllocatableMem > 0 {
		cm.AvgMemUtilization = cm.TotalRequestedMem / cm.TotalAllocatableMem
	}

	// Find fragmented and hot nodes
	for _, n := range cm.Nodes {
		if n.CPUUtilization < 0.3 && n.MemUtilization < 0.3 && n.PodCount > 0 {
			cm.FragmentedNodes = append(cm.FragmentedNodes, n.NodeName)
		}
		if n.CPUUtilization > 0.85 || n.MemUtilization > 0.85 {
			cm.HotNodes = append(cm.HotNodes, n.NodeName)
		}
	}

	return cm, nil
}

// CollectNodeScalingRecommendation calculates how many nodes to add or remove.
func (pc *PromCollector) CollectNodeScalingRecommendation(targetCPU, targetMem float64) *NodeScalingResult {
	cm, err := pc.CollectClusterMetrics()
	if err != nil || cm.TotalNodes == 0 {
		return &NodeScalingResult{CurrentNodes: 0}
	}

	result := &NodeScalingResult{
		CurrentNodes:        cm.TotalNodes,
		AvgCPUUtilization:   cm.AvgCPUUtilization,
		AvgMemUtilization:   cm.AvgMemUtilization,
		TotalAllocatableCPU: cm.TotalAllocatableCPU,
		TotalRequestedCPU:   cm.TotalRequestedCPU,
		FragmentedNodes:     cm.FragmentedNodes,
		HotNodes:            cm.HotNodes,
	}

	if cm.TotalAllocatableCPU > 0 && cm.TotalNodes > 0 {
		avgCPUPerNode := cm.TotalAllocatableCPU / float64(cm.TotalNodes)
		neededCPUCapacity := cm.TotalRequestedCPU / targetCPU
		result.RecommendedNodes = int(neededCPUCapacity/avgCPUPerNode + 0.5)
		if result.RecommendedNodes < 1 {
			result.RecommendedNodes = 1
		}
	}

	result.CanRemoveNodes = result.RecommendedNodes < result.CurrentNodes
	if result.CanRemoveNodes {
		result.NodesToRemove = result.CurrentNodes - result.RecommendedNodes
	}
	result.NeedMoreNodes = result.RecommendedNodes > result.CurrentNodes
	if result.NeedMoreNodes {
		result.NodesToAdd = result.RecommendedNodes - result.CurrentNodes
	}

	// Rough cost savings estimate
	if result.CanRemoveNodes && cm.TotalNodes > 0 {
		avgCostPerNode := 100.0 // default monthly cost per node
		result.MonthlySavings = float64(result.NodesToRemove) * avgCostPerNode
	}

	return result
}

// CollectLabelCosts attributes costs by namespace and app-group (deployment prefix).
// Falls back to namespace/app grouping since kube_deployment_labels may not be available.
func (pc *PromCollector) CollectLabelCosts(namespaces []string, _ []string) map[string]*LabelCostEntry {
	result := make(map[string]*LabelCostEntry)

	for _, ns := range namespaces {
		query := fmt.Sprintf(`kube_deployment_created{namespace="%s"}`, ns)
		u := fmt.Sprintf("%s/api/v1/query?query=%s", pc.Address, url.QueryEscape(query))
		data, err := pc.doQuery(u)
		if err != nil {
			continue
		}

		for _, r := range data {
			deploy := r.Metric["deployment"]
			if deploy == "" {
				continue
			}

			// Attribute by namespace
			nsKey := "namespace=" + ns
			if result[nsKey] == nil {
				result[nsKey] = &LabelCostEntry{LabelKey: "namespace", LabelValue: ns}
			}

			// Attribute by app group (deployment prefix before first "-")
			appGroup := extractAppGroup(deploy)
			appKey := "app=" + appGroup
			if result[appKey] == nil {
				result[appKey] = &LabelCostEntry{LabelKey: "app", LabelValue: appGroup}
			}

			cpuReq := pc.queryScalar(fmt.Sprintf(
				`max(kube_pod_container_resource_requests{namespace="%s", pod=~"%s-.*", resource="cpu"}) * 1000`, ns, deploy))
			memReq := pc.queryScalar(fmt.Sprintf(
				`max(kube_pod_container_resource_requests{namespace="%s", pod=~"%s-.*", resource="memory"}) / 1024 / 1024`, ns, deploy))
			replicas := pc.queryScalar(fmt.Sprintf(
				`kube_deployment_spec_replicas{namespace="%s", deployment="%s"}`, ns, deploy))
			if replicas < 1 {
				replicas = 1
			}

			podCost := (cpuReq/1000.0)*replicas*20.0 + (memReq/1024.0)*replicas*2.0
			svcFullName := ns + "/" + deploy

			addToEntry(result[nsKey], svcFullName, podCost)
			addToEntry(result[appKey], svcFullName, podCost)
		}
	}
	return result
}

func addToEntry(entry *LabelCostEntry, svcName string, cost float64) {
	for _, s := range entry.Services {
		if s == svcName {
			return
		}
	}
	entry.ServiceCount++
	entry.Services = append(entry.Services, svcName)
	entry.PodCost += cost
	entry.TotalCost += cost
}

func (pc *PromCollector) discoverNodes() ([]string, error) {
	query := url.QueryEscape("kube_node_info")
	u := fmt.Sprintf("%s/api/v1/query?query=%s", pc.Address, query)
	data, err := pc.doQuery(u)
	if err != nil {
		return nil, err
	}
	var nodes []string
	seen := map[string]bool{}
	for _, r := range data {
		if node, ok := r.Metric["node"]; ok && node != "" && !seen[node] {
			nodes = append(nodes, node)
			seen[node] = true
		}
	}
	return nodes, nil
}

// CollectServiceHistory collects RPS/cpu/mem time-series for forecast.
func (pc *PromCollector) CollectServiceHistory(namespace, deployment string) (cpuPoints, memPoints, rpsPoints []TimeSeriesPoint, err error) {
	// Use the doQuery approach for instant queries
	cpuQuery := fmt.Sprintf(
		`sum(rate(container_cpu_usage_seconds_total{namespace="%s", pod=~"%s-.*", container!=""}[5m])) * 1000`,
		namespace, deployment)
	u := fmt.Sprintf("%s/api/v1/query?query=%s", pc.Address, url.QueryEscape(cpuQuery))
	data, err := pc.doQuery(u)
	if err == nil && len(data) > 0 {
		val, _ := strconv.ParseFloat(data[0].Value, 64)
		cpuPoints = append(cpuPoints, TimeSeriesPoint{Timestamp: 0, Value: val})
	}

	memQuery := fmt.Sprintf(
		`sum(container_memory_working_set_bytes{namespace="%s", pod=~"%s-.*", container!=""}) / 1024 / 1024`,
		namespace, deployment)
	u = fmt.Sprintf("%s/api/v1/query?query=%s", pc.Address, url.QueryEscape(memQuery))
	data, err = pc.doQuery(u)
	if err == nil && len(data) > 0 {
		val, _ := strconv.ParseFloat(data[0].Value, 64)
		memPoints = append(memPoints, TimeSeriesPoint{Timestamp: 0, Value: val})
	}

	rpsQuery := fmt.Sprintf(
		`sum(rate(traefik_service_requests_total{exported_service=~"%s-%s-.*"}[5m]))`,
		namespace, deployment)
	u = fmt.Sprintf("%s/api/v1/query?query=%s", pc.Address, url.QueryEscape(rpsQuery))
	data, err = pc.doQuery(u)
	if err == nil && len(data) > 0 {
		val, _ := strconv.ParseFloat(data[0].Value, 64)
		rpsPoints = append(rpsPoints, TimeSeriesPoint{Timestamp: 0, Value: val})
	}

	return cpuPoints, memPoints, rpsPoints, nil
}

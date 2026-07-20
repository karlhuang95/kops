package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	appanalyze "kops/internal/app/analyze"
	platformcollector "kops/internal/platform/collector"
	platformconfig "kops/internal/platform/config"
)

const topologyFileName = "topology.json"

// TopologyStore writes Kubernetes topology and analysis data to a local JSON file.
type TopologyStore struct {
	filePath string
	cfg      *platformconfig.GlobalConfig
	// in-memory graph built during PopulateAll
	graph *GraphData
}

// NewTopologyStore creates a local file-backed topology store. Returns nil if not configured.
func NewTopologyStore(ctx context.Context, cfg *platformconfig.GlobalConfig) (*TopologyStore, error) {
	dir := cfg.Storage.Path
	if dir == "" {
		return nil, nil
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("storage dir: %w", err)
	}

	fp := filepath.Join(dir, topologyFileName)
	slog.Info("topology store initialized", "path", fp)
	return &TopologyStore{filePath: fp, cfg: cfg}, nil
}

// PopulateAll builds the full topology graph from all data sources and writes to file.
func (ts *TopologyStore) PopulateAll(ctx context.Context, data *appanalyze.AnalysisData) error {
	if data == nil {
		return nil
	}

	gd := &GraphData{
		Nodes: make([]GraphNode, 0),
		Edges: make([]GraphEdge, 0),
	}

	// Phase 1: K8s resource topology + analysis data
	ts.populateAnalysisData(gd, data)

	// Phase 2: Jaeger service dependencies
	ts.populateJaegerTopology(ctx, gd)

	// Phase 3: Traefik entrypoint → service routing
	ts.populateTraefikTopology(ctx, gd)

	// Phase 4: Aggregate call stats onto Deployment nodes.
	ts.aggregateCallStats(gd)

	// Write to file
	if err := ts.writeGraph(gd); err != nil {
		return fmt.Errorf("write topology: %w", err)
	}

	ts.graph = gd
	slog.Info("topology populated",
		"deployments", len(data.AdvisorResults),
		"healthStatuses", len(data.HealthStatuses),
		"nodes", len(gd.Nodes),
		"edges", len(gd.Edges))
	return nil
}

// populateAnalysisData creates Namespace and Deployment nodes with analysis properties.
func (ts *TopologyStore) populateAnalysisData(gd *GraphData, data *appanalyze.AnalysisData) {
	nsSet := make(map[string]bool)
	type depInfo struct {
		namespace  string
		advisor    *AdviceResultRef
		efficiency *EfficiencyResultRef
		health     *HealthStatusRef
	}
	depMap := make(map[string]*depInfo)

	addDep := func(ns, dep string) *depInfo {
		key := ns + "/" + dep
		if _, ok := depMap[key]; !ok {
			depMap[key] = &depInfo{namespace: ns}
		}
		return depMap[key]
	}

	for i := range data.AdvisorResults {
		r := &data.AdvisorResults[i]
		nsSet[r.Namespace] = true
		info := addDep(r.Namespace, r.Deployment)
		info.advisor = &AdviceResultRef{
			OldCPURequest:   r.OldCPURequest,
			OldCPULimit:     r.OldCPULimit,
			OldMemRequest:   r.OldMemRequest,
			OldMemLimit:     r.OldMemLimit,
			CPUUsageMax:     r.CPUUsageMax,
			CPUUsageAvg:     r.CPUUsageAvg,
			MemUsageMax:     r.MemUsageMax,
			ThrottleSecond:  r.ThrottleSecond,
			AvgRPS:          r.AvgRPS,
			NewCPURequest:   r.NewCPURequest,
			NewMemRequest:   r.NewMemRequest,
			CurrentCost:     r.CurrentCost,
			RecommendedCost: r.RecommendedCost,
			ActualCost:      r.ActualCost,
			MonthlySaving:   r.MonthlySaving,
			Efficiency:      r.Efficiency,
			RPSDensity:      r.RPSDensity,
			PriorityScore:   r.PriorityScore,
			Action:          r.Action,
			Reason:          r.Reason,
			RiskLevel:       r.RiskLevel,
			IsHighRisk:      r.IsHighRisk,
			IsInefficient:   r.IsInefficient,
			IsProtected:     r.IsProtected,
			IsBlackHole:     r.IsBlackHole,
			AppGroup:        r.AppGroup,
		}
	}

	for i := range data.EfficiencyResults {
		r := &data.EfficiencyResults[i]
		info := addDep(r.Namespace, r.ServiceName)
		info.efficiency = &EfficiencyResultRef{
			CurrentCPU:         r.CurrentCPU,
			CurrentMem:         r.CurrentMem,
			Replicas:           r.Replicas,
			TrafficDensity:     r.TrafficDensity,
			TrafficDensityRank: r.TrafficDensityRank,
			RecCPU:             r.RecCPU,
			RecMem:             r.RecMem,
			CurrentCost:        r.CurrentCost,
			RecCost:            r.RecCost,
			ActualCost:         r.ActualCost,
			MonthlySaving:      r.MonthlySaving,
			WasteAmount:        r.WasteAmount,
			WasteRatio:         r.WasteRatio,
		}
	}

	for i := range data.HealthStatuses {
		r := &data.HealthStatuses[i]
		nsSet[r.Namespace] = true
		info := addDep(r.Namespace, r.ServiceName)
		info.health = &HealthStatusRef{
			RPS:          r.RPS,
			Error5xxRate: r.Error5xxRate,
			Error4xxRate: r.Error4xxRate,
			P99Latency:   r.P99Latency,
			HealthCode:   r.HealthCode,
			HealthScore:  r.HealthScore,
			InvalidSpend: r.InvalidSpend,
			Diagnosis:    r.Diagnosis,
		}
	}

	// Create Namespace nodes
	for ns := range nsSet {
		gd.Nodes = append(gd.Nodes, GraphNode{
			ID:    ns,
			Label: "Namespace",
			Group: "Namespace",
			Properties: map[string]interface{}{
				"name": ns,
			},
		})
	}

	// Create Deployment nodes with properties and CONTAINS edges
	for key, info := range depMap {
		props := map[string]interface{}{
			"name":      key,
			"namespace": info.namespace,
		}

		if info.advisor != nil {
			props["riskLevel"] = info.advisor.RiskLevel
			props["currentCost"] = info.advisor.CurrentCost
			props["recommendedCost"] = info.advisor.RecommendedCost
			props["monthlySaving"] = info.advisor.MonthlySaving
			props["cpuUsageAvg"] = info.advisor.CPUUsageAvg
			props["cpuUsageMax"] = info.advisor.CPUUsageMax
			props["memUsageMax"] = info.advisor.MemUsageMax
			props["avgRPS"] = info.advisor.AvgRPS
			props["newCPURequest"] = info.advisor.NewCPURequest
			props["newMemRequest"] = info.advisor.NewMemRequest
			props["oldCPURequest"] = info.advisor.OldCPURequest
			props["oldMemRequest"] = info.advisor.OldMemRequest
			props["isHighRisk"] = info.advisor.IsHighRisk
			props["isInefficient"] = info.advisor.IsInefficient
			props["isBlackHole"] = info.advisor.IsBlackHole
			props["action"] = info.advisor.Action
			props["reason"] = info.advisor.Reason
			props["efficiency"] = info.advisor.Efficiency
			props["rpsDensity"] = info.advisor.RPSDensity
			props["appGroup"] = info.advisor.AppGroup
			props["priorityScore"] = info.advisor.PriorityScore
		}
		if info.efficiency != nil {
			props["trafficDensity"] = info.efficiency.TrafficDensity
			props["trafficDensityRank"] = info.efficiency.TrafficDensityRank
			props["wasteAmount"] = info.efficiency.WasteAmount
			props["wasteRatio"] = info.efficiency.WasteRatio
		}
		if info.health != nil {
			props["healthCode"] = info.health.HealthCode
			props["healthScore"] = info.health.HealthScore
			props["error5xxRate"] = info.health.Error5xxRate
			props["p99Latency"] = info.health.P99Latency
			props["invalidSpend"] = info.health.InvalidSpend
			props["diagnosis"] = info.health.Diagnosis
		}

		gd.Nodes = append(gd.Nodes, GraphNode{
			ID:         key,
			Label:      "Deployment",
			Group:      "Deployment",
			Properties: props,
		})

		// CONTAINS edge: Namespace → Deployment
		gd.Edges = append(gd.Edges, GraphEdge{
			Source: info.namespace,
			Target: key,
			Type:   "CONTAINS",
		})
	}
}

// depInfo holds parsed deployment key information for Jaeger matching.
type depInfo struct{ ns, name, env string }

// populateJaegerTopology fetches service dependencies from Jaeger and creates CALLS edges
// directly between Deployment nodes where possible, falling back to JaegerService nodes.
func (ts *TopologyStore) populateJaegerTopology(ctx context.Context, gd *GraphData) {
	jc := platformcollector.NewJaegerCollector(ts.cfg)
	if jc == nil {
		return
	}

	deps, err := jc.CollectDependencies(time.Hour)
	if err != nil {
		slog.Warn("jaeger topology skipped", "err", err)
		return
	}

	// Build a lookup: deployment key → parsed (namespace, deploymentName, env)
	depLookup := make(map[string]depInfo)
	for _, n := range gd.Nodes {
		if n.Group == "Deployment" {
			parts := strings.SplitN(n.ID, "/", 2)
			if len(parts) != 2 {
				continue
			}
			depLookup[n.ID] = depInfo{ns: parts[0], name: parts[1], env: splitNsEnv(parts[0])}
		}
	}
	nsEnvs := ts.namespaceEnvs()

	// seenCallKeys deduplicates CALLS edges between the same deployment pair.
	seenCallKeys := make(map[string]bool)
	// jaegerNodes tracks fallback JaegerService nodes created for unmatched services.
	jaegerNodes := make(map[string]bool)
	directEdges := 0
	fallbackEdges := 0

	for _, dep := range deps {
		if dep.Parent == "" || dep.Child == "" {
			continue
		}
		if dep.Parent == "empty-service-name" || dep.Child == "empty-service-name" ||
			dep.Parent == "jaeger" || dep.Child == "jaeger" {
			continue
		}

		parentDep := ts.resolveJaegerToDeployment(dep.Parent, depLookup, nsEnvs)
		childDep := ts.resolveJaegerToDeployment(dep.Child, depLookup, nsEnvs)

		if parentDep != "" && childDep != "" && parentDep != childDep {
			// Both resolved to Deployment nodes — create direct CALLS edge.
			callKey := parentDep + "→" + childDep
			if seenCallKeys[callKey] {
				continue
			}
			seenCallKeys[callKey] = true
			gd.Edges = append(gd.Edges, GraphEdge{
				Source: parentDep,
				Target: childDep,
				Type:   "CALLS",
				Properties: map[string]interface{}{
					"callCount": dep.CallCount,
				},
			})
			directEdges++
		} else {
			// Fallback: create JaegerService nodes and CALLS edge between them.
			for _, name := range []string{dep.Parent, dep.Child} {
				if !jaegerNodes[name] {
					jaegerNodes[name] = true
					matchedDep := ts.resolveJaegerToDeployment(name, depLookup, nsEnvs)
					props := map[string]interface{}{"name": name}
					if matchedDep != "" {
						props["matchedDeployment"] = matchedDep
					}
					gd.Nodes = append(gd.Nodes, GraphNode{
						ID:         name,
						Label:      "JaegerService",
						Group:      "JaegerService",
						Properties: props,
					})
				}
			}
			gd.Edges = append(gd.Edges, GraphEdge{
				Source: dep.Parent,
				Target: dep.Child,
				Type:   "CALLS",
				Properties: map[string]interface{}{
					"callCount": dep.CallCount,
				},
			})
			fallbackEdges++
		}
	}

	slog.Info("jaeger topology written", "directEdges", directEdges, "fallbackEdges", fallbackEdges)
}

// resolveJaegerToDeployment tries to match a Jaeger service name to a known Deployment node ID.
// Returns the deployment key (e.g. "ns-prod/my-service-prod") or empty string if no match.
func (ts *TopologyStore) resolveJaegerToDeployment(jsName string, depLookup map[string]depInfo, nsEnvs map[string]string) string {
	base, env := parseJaegerService(jsName)
	if env == "" {
		return ""
	}

	// Normalize: lowercase, replace underscores with hyphens
	normalizedBase := strings.ToLower(strings.ReplaceAll(base, "_", "-"))

	// Pass 1: exact match on normalized base name within the same environment.
	for depKey, info := range depLookup {
		if info.env != env {
			continue
		}
		depBase := strings.TrimSuffix(info.name, "-"+env)
		depBaseNorm := strings.ToLower(strings.ReplaceAll(depBase, "_", "-"))
		if depBaseNorm == normalizedBase {
			return depKey
		}
	}

	// Pass 2: the deployment name contains the Jaeger base or vice versa.
	for depKey, info := range depLookup {
		if info.env != env {
			continue
		}
		depBase := strings.TrimSuffix(info.name, "-"+env)
		depBaseNorm := strings.ToLower(strings.ReplaceAll(depBase, "_", "-"))
		if strings.Contains(depBaseNorm, normalizedBase) || strings.Contains(normalizedBase, depBaseNorm) {
			return depKey
		}
	}

	// Pass 3: try matching Jaeger base segments against deployment name segments.
	jaegerSegments := strings.Split(normalizedBase, "-")
	for depKey, info := range depLookup {
		if info.env != env {
			continue
		}
		depBase := strings.TrimSuffix(info.name, "-"+env)
		depBaseNorm := strings.ToLower(strings.ReplaceAll(depBase, "_", "-"))
		depSegments := strings.Split(depBaseNorm, "-")
		// If all segments of one appear in the other, it's a match.
		if len(jaegerSegments) >= 2 && containsAll(depSegments, jaegerSegments) {
			return depKey
		}
		if len(depSegments) >= 2 && containsAll(jaegerSegments, depSegments) {
			return depKey
		}
	}

	return ""
}

// containsAll returns true if all elements of sub are present in super.
func containsAll(super, sub []string) bool {
	superSet := make(map[string]bool, len(super))
	for _, s := range super {
		superSet[s] = true
	}
	for _, s := range sub {
		if !superSet[s] {
			return false
		}
	}
	return true
}

// populateTraefikTopology queries Prometheus for Traefik traffic data and creates flow nodes.
func (ts *TopologyStore) populateTraefikTopology(ctx context.Context, gd *GraphData) {
	if ts.cfg.Prometheus.Address == "" {
		return
	}

	pc := platformcollector.NewCollector(ts.cfg)

	// Query entrypoint traffic
	epQuery := `sum by(entrypoint)(rate(traefik_entrypoint_requests_total[24h]))`
	epData, err := pc.QueryInstant(epQuery)
	if err != nil {
		slog.Warn("traefik entrypoint query failed", "err", err)
		return
	}

	// External node
	gd.Nodes = append(gd.Nodes, GraphNode{
		ID:    "Internet",
		Label: "External",
		Group: "External",
		Properties: map[string]interface{}{
			"name": "Internet",
		},
	})

	for _, r := range epData {
		ep := r.Metric["entrypoint"]
		if ep == "" || ep == "metrics" {
			continue
		}
		gd.Nodes = append(gd.Nodes, GraphNode{
			ID:    ep,
			Label: "Entrypoint",
			Group: "Entrypoint",
			Properties: map[string]interface{}{
				"name": ep,
			},
		})
		gd.Edges = append(gd.Edges, GraphEdge{
			Source: "Internet",
			Target: ep,
			Type:   "ROUTES_TO",
			Properties: map[string]interface{}{
				"rps": r.Value,
			},
		})
	}

	// Query backend service traffic
	svcQuery := `topk(50, sum by(exported_service)(rate(traefik_service_requests_total[24h])))`
	svcData, err := pc.QueryInstant(svcQuery)
	if err != nil {
		slog.Warn("traefik service query failed", "err", err)
		return
	}

	nsSet := make(map[string]bool)
	for _, ns := range ts.cfg.Namespaces {
		nsSet[ns] = true
	}

	trafficCount := 0
	for _, r := range svcData {
		exportedSvc := r.Metric["exported_service"]
		ns, svcName := parseExportedService(exportedSvc)
		if ns == "" || svcName == "" {
			continue
		}
		if !nsSet[ns] {
			continue
		}

		rps := r.Value
		if rps < 0.001 {
			continue
		}

		svcKey := ns + "/" + svcName
		gd.Nodes = append(gd.Nodes, GraphNode{
			ID:    svcKey,
			Label: "K8sService",
			Group: "K8sService",
			Properties: map[string]interface{}{
				"name":      svcKey,
				"namespace": ns,
				"rps":       rps,
			},
		})

		// CONTAINS edge: Namespace → K8sService
		gd.Edges = append(gd.Edges, GraphEdge{
			Source: ns,
			Target: svcKey,
			Type:   "CONTAINS",
		})

		// ROUTES_TO edge: Entrypoints → K8sService
		for _, n := range gd.Nodes {
			if n.Group == "Entrypoint" && (n.ID == "web" || n.ID == "websecure") {
				gd.Edges = append(gd.Edges, GraphEdge{
					Source: n.ID,
					Target: svcKey,
					Type:   "ROUTES_TO",
					Properties: map[string]interface{}{
						"rps": rps,
					},
				})
			}
		}
		trafficCount++
	}

	slog.Info("traefik topology written", "entrypoints", len(epData), "trafficServices", trafficCount)
}

// aggregateCallStats computes inbound/outbound call counts and connected peers
// for each Deployment node based on CALLS edges, storing them as node properties.
func (ts *TopologyStore) aggregateCallStats(gd *GraphData) {
	// Map deployment ID → {inbound count, outbound count, peer set}
	type callStats struct {
		inbound  int
		outbound int
		peers    map[string]bool
	}
	stats := make(map[string]*callStats)

	ensure := func(id string) *callStats {
		if s, ok := stats[id]; ok {
			return s
		}
		s := &callStats{peers: make(map[string]bool)}
		stats[id] = s
		return s
	}

	for _, e := range gd.Edges {
		if e.Type != "CALLS" {
			continue
		}
		cc := 0
		if v, ok := e.Properties["callCount"]; ok {
			switch vv := v.(type) {
			case int:
				cc = vv
			case float64:
				cc = int(vv)
			}
		}
		src := ensure(e.Source)
		src.outbound += cc
		src.peers[e.Target] = true
		dst := ensure(e.Target)
		dst.inbound += cc
		dst.peers[e.Source] = true
	}

	for i, n := range gd.Nodes {
		if n.Group != "Deployment" {
			continue
		}
		s, ok := stats[n.ID]
		if !ok {
			continue
		}
		if n.Properties == nil {
			n.Properties = make(map[string]interface{})
		}
		n.Properties["callInbound"] = s.inbound
		n.Properties["callOutbound"] = s.outbound
		n.Properties["callPeerCount"] = len(s.peers)
		gd.Nodes[i] = n
	}
}

// GetGraphData returns the full topology graph, reading from the local file.
func (ts *TopologyStore) GetGraphData(ctx context.Context) (*GraphData, error) {
	// Return in-memory copy if available
	if ts.graph != nil {
		return ts.graph, nil
	}

	// Otherwise read from file
	return ts.readGraph()
}

// ClearAll removes the topology data file.
func (ts *TopologyStore) ClearAll(ctx context.Context) error {
	ts.graph = nil
	if err := os.Remove(ts.filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove topology file: %w", err)
	}
	slog.Info("topology data cleared")
	return nil
}

// Close is a no-op for local file storage.
func (ts *TopologyStore) Close(ctx context.Context) error {
	return nil
}

// ---- File I/O ----

func (ts *TopologyStore) writeGraph(gd *GraphData) error {
	data, err := json.MarshalIndent(gd, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal topology: %w", err)
	}
	if err := os.WriteFile(ts.filePath, data, 0644); err != nil {
		return fmt.Errorf("write topology file: %w", err)
	}
	return nil
}

func (ts *TopologyStore) readGraph() (*GraphData, error) {
	data, err := os.ReadFile(ts.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &GraphData{Nodes: []GraphNode{}, Edges: []GraphEdge{}}, nil
		}
		return nil, fmt.Errorf("read topology file: %w", err)
	}

	gd := &GraphData{}
	if err := json.Unmarshal(data, gd); err != nil {
		return nil, fmt.Errorf("unmarshal topology: %w", err)
	}
	return gd, nil
}

// ---- Helper functions ----

func (ts *TopologyStore) namespaceEnvs() map[string]string {
	envs := make(map[string]string)
	for _, ns := range ts.cfg.Namespaces {
		parts := splitNsEnv(ns)
		if parts != "" {
			envs[ns] = parts
		}
	}
	return envs
}

func splitNsEnv(ns string) string {
	for _, env := range []string{"prod", "fat", "staging", "dev", "uat"} {
		if len(ns) > len(env)+1 && ns[len(ns)-len(env):] == env && ns[len(ns)-len(env)-1] == '-' {
			return env
		}
	}
	return ""
}

func parseJaegerService(name string) (base, env string) {
	for _, e := range []string{"prod", "fat", "staging", "dev", "uat"} {
		suffix := "_" + e
		if len(name) > len(suffix) && name[len(name)-len(suffix):] == suffix {
			return name[:len(name)-len(suffix)], e
		}
	}
	return name, ""
}

func parseExportedService(raw string) (namespace, service string) {
	if idx := strings.Index(raw, "@"); idx >= 0 {
		raw = raw[:idx]
	}
	parts := strings.Split(raw, "-")
	if len(parts) < 3 {
		return "", ""
	}
	hashPart := parts[len(parts)-1]
	if len(hashPart) < 10 {
		return "", ""
	}
	nsPrefix := parts[0]
	envs := []string{"prod", "fat", "staging", "dev", "uat"}
	for i := 1; i < len(parts)-1; i++ {
		for _, env := range envs {
			if parts[i] == env {
				namespace = nsPrefix + "-" + env
				service = strings.Join(parts[i+1:len(parts)-1], "-")
				return namespace, service
			}
		}
	}
	if len(parts) >= 3 {
		namespace = parts[0] + "-" + parts[1]
		service = strings.Join(parts[2:len(parts)-1], "-")
	}
	return namespace, service
}

// ---- Graph data types (JSON-friendly) ----

// GraphData is the JSON response for the topology visualization.
type GraphData struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// GraphNode represents a single node in the topology graph.
type GraphNode struct {
	ID         string                 `json:"id"`
	Label      string                 `json:"label"`
	Group      string                 `json:"group"`
	Properties map[string]interface{} `json:"properties"`
}

// GraphEdge represents a directed edge in the topology graph.
type GraphEdge struct {
	Source     string                 `json:"source"`
	Target     string                 `json:"target"`
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
}

// ---- Reference types for passing analysis data ----

// AdviceResultRef is a lightweight reference to advisor result fields.
type AdviceResultRef struct {
	OldCPURequest, OldCPULimit, OldMemRequest, OldMemLimit int
	CPUUsageMax, CPUUsageAvg, MemUsageMax                  float64
	ThrottleSecond, AvgRPS                                 float64
	NewCPURequest, NewMemRequest                           int
	CurrentCost, RecommendedCost, ActualCost, MonthlySaving float64
	Efficiency, RPSDensity, PriorityScore                  float64
	Action, Reason, RiskLevel, AppGroup                    string
	IsHighRisk, IsInefficient, IsProtected, IsBlackHole    bool
}

// EfficiencyResultRef is a lightweight reference to efficiency result fields.
type EfficiencyResultRef struct {
	CurrentCPU, CurrentMem, Replicas                     int
	TrafficDensity                                       float64
	TrafficDensityRank                                   string
	RecCPU, RecMem                                       int
	CurrentCost, RecCost, ActualCost                     float64
	MonthlySaving, WasteAmount, WasteRatio               float64
}

// HealthStatusRef is a lightweight reference to health status fields.
type HealthStatusRef struct {
	RPS, Error5xxRate, Error4xxRate, P99Latency float64
	HealthCode, Diagnosis                       string
	HealthScore                                 float64
	InvalidSpend                                float64
}

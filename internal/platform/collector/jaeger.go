package collector

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	platformconfig "kops/internal/platform/config"
)

// JaegerCollector fetches service topology from Jaeger tracing API.
type JaegerCollector struct {
	address string
	timeout time.Duration
}

// JaegerServiceDependency represents a directed call edge between two services.
type JaegerServiceDependency struct {
	Parent    string `json:"parent"`
	Child     string `json:"child"`
	CallCount int    `json:"callCount"`
}

// NewJaegerCollector creates a collector from config. Returns nil if not configured.
func NewJaegerCollector(cfg *platformconfig.GlobalConfig) *JaegerCollector {
	if cfg.Jaeger.Address == "" {
		return nil
	}
	timeout := 30 * time.Second
	if d, err := time.ParseDuration(cfg.Jaeger.Timeout); err == nil && d > 0 {
		timeout = d
	}
	return &JaegerCollector{
		address: strings.TrimRight(cfg.Jaeger.Address, "/"),
		timeout: timeout,
	}
}

// CollectServices returns all service names known to Jaeger.
func (jc *JaegerCollector) CollectServices() ([]string, error) {
	url := fmt.Sprintf("%s/api/services", jc.address)
	body, err := jc.doGet(url)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("jaeger services: %w", err)
	}
	return resp.Data, nil
}

// CollectDependencies returns the service dependency graph for the given lookback.
// Tries /api/dependencies first; if it fails or returns empty, extracts deps from traces.
func (jc *JaegerCollector) CollectDependencies(lookback time.Duration) ([]JaegerServiceDependency, error) {
	endTs := time.Now().UnixMilli()
	lookbackMs := lookback.Milliseconds()

	url := fmt.Sprintf("%s/api/dependencies?endTs=%d&lookback=%d",
		jc.address, endTs, lookbackMs)

	body, err := jc.doGet(url)
	if err == nil {
		var resp struct {
			Data []JaegerServiceDependency `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err == nil && len(resp.Data) > 0 {
			return resp.Data, nil
		}
	}

	// Dependencies API unavailable or empty — build deps from traces.
	return jc.CollectDependenciesFromTraces(lookback)
}

// CollectDependenciesFromTraces builds a service dependency graph by sampling traces
// and extracting parent→child service relationships from span references.
func (jc *JaegerCollector) CollectDependenciesFromTraces(lookback time.Duration) ([]JaegerServiceDependency, error) {
	services, err := jc.CollectServices()
	if err != nil {
		return nil, fmt.Errorf("jaeger services for trace deps: %w", err)
	}

	skip := map[string]bool{"empty-service-name": true, "jaeger": true}
	var relevant []string
	for _, s := range services {
		if !skip[s] && !strings.HasPrefix(s, "unknown_service") {
			relevant = append(relevant, s)
		}
	}

	slog.Info("building deps from traces", "services", len(relevant))

	type depKey struct{ parent, child string }
	var (
		mu         sync.Mutex
		depCounts  = make(map[depKey]int)
		seenTraces = make(map[string]bool)
		wg         sync.WaitGroup
		sem        = make(chan struct{}, 5) // max 5 concurrent requests
	)
	lookbackStr := lookbackToJaegerStr(lookback)

	for _, svc := range relevant {
		wg.Add(1)
		go func(svc string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			url := fmt.Sprintf("%s/api/traces?service=%s&limit=10&lookback=%s",
				jc.address, svc, lookbackStr)

			body, err := jc.doGet(url)
			if err != nil {
				return
			}

			var tracesResp struct {
				Data []jaegerTrace `json:"data"`
			}
			if err := json.Unmarshal(body, &tracesResp); err != nil {
				return
			}

			for _, trace := range tracesResp.Data {
				mu.Lock()
				if seenTraces[trace.TraceID] {
					mu.Unlock()
					continue
				}
				seenTraces[trace.TraceID] = true
				mu.Unlock()

				pidToSvc := make(map[string]string)
				for pid, proc := range trace.Processes {
					pidToSvc[pid] = proc.ServiceName
				}
				spanToPid := make(map[string]string)
				for _, span := range trace.Spans {
					spanToPid[span.SpanID] = span.ProcessID
				}

				for _, span := range trace.Spans {
					childSvc := pidToSvc[span.ProcessID]
					if childSvc == "" {
						continue
					}
					for _, ref := range span.References {
						if ref.RefType == "CHILD_OF" {
							parentPid := spanToPid[ref.SpanID]
							parentSvc := pidToSvc[parentPid]
							if parentSvc != "" && parentSvc != childSvc {
								mu.Lock()
								depCounts[depKey{parentSvc, childSvc}]++
								mu.Unlock()
							}
						}
					}
				}
			}
		}(svc)
	}
	wg.Wait()

	var result []JaegerServiceDependency
	for k, count := range depCounts {
		result = append(result, JaegerServiceDependency{
			Parent:    k.parent,
			Child:     k.child,
			CallCount: count,
		})
	}

	slog.Info("trace deps built", "dependencies", len(result), "services_queried", len(relevant))
	return result, nil
}

// jaegerTrace is the raw trace JSON returned by /api/traces.
type jaegerTrace struct {
	TraceID   string                   `json:"traceID"`
	Spans     []jaegerSpan             `json:"spans"`
	Processes map[string]jaegerProcess `json:"processes"`
}

type jaegerSpan struct {
	SpanID     string          `json:"spanID"`
	ProcessID  string          `json:"processID"`
	References []jaegerRef     `json:"references"`
}

type jaegerRef struct {
	RefType string `json:"refType"`
	SpanID  string `json:"spanID"`
}

type jaegerProcess struct {
	ServiceName string `json:"serviceName"`
}

// lookbackToJaegerStr converts a duration to a Jaeger-friendly string.
func lookbackToJaegerStr(d time.Duration) string {
	h := d.Hours()
	if h >= 24 {
		return fmt.Sprintf("%dd", int(h/24))
	}
	if h >= 1 {
		return fmt.Sprintf("%dh", int(h))
	}
	m := d.Minutes()
	if m >= 1 {
		return fmt.Sprintf("%dm", int(m))
	}
	return "5m"
}

func (jc *JaegerCollector) doGet(url string) ([]byte, error) {
	client := &http.Client{Timeout: jc.timeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("jaeger request %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jaeger returned %d", resp.StatusCode)
	}

	var body json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("jaeger decode: %w", err)
	}
	return body, nil
}

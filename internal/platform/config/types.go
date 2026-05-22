package config

// GlobalConfig 对应整个 config.yaml
type GlobalConfig struct {
	Namespaces []string `mapstructure:"namespaces"`
	Prometheus struct {
		Address string `mapstructure:"address"`
		Timeout string `mapstructure:"timeout"`
		Range   string `mapstructure:"range"`
		Step    string `mapstructure:"step"`
	} `mapstructure:"prometheus"`
	Cost        CostSpec     `mapstructure:"cost"`
	Governance  GovSpec      `mapstructure:"governance"`
	GatewayCost GatewaySpec  `mapstructure:"gateway_cost"`
	FinOps      FinOpsConfig `mapstructure:"finops"`
}

type CostSpec struct {
	Price          float64        `mapstructure:"price"`
	Count          int            `mapstructure:"count"`
	CPUCores       int            `mapstructure:"cpu_cores"`
	MemoryGB       int            `mapstructure:"memory_gb"`
	ResourceWeight ResourceWeight `mapstructure:"resource_weight"`
}

type ResourceWeight struct {
	CPU    float64 `mapstructure:"cpu"`
	Memory float64 `mapstructure:"memory"`
}

type GovSpec struct {
	TargetUtilization float64 `mapstructure:"target_utilization"`
	CPUStep           int     `mapstructure:"cpu_step"`
	MemoryStep        int     `mapstructure:"memory_step"`
	MinCPU            int     `mapstructure:"min_cpu"`
	MinMemory         int     `mapstructure:"min_memory"`
	CPULimitFactor    float64 `mapstructure:"cpu_limit_factor"`
	MemLimitFactor    float64 `mapstructure:"mem_limit_factor"`

	RPSLowThreshold       float64 `mapstructure:"rps_low_threshold"`
	CPULowThreshold       float64 `mapstructure:"cpu_low_threshold"`
	ThrottleHighThreshold float64 `mapstructure:"throttle_high_threshold"`
	MemHighThreshold      float64 `mapstructure:"mem_high_threshold"`
	P95LowThreshold       float64 `mapstructure:"p95_low_threshold"`
	RPSDensityThreshold   float64 `mapstructure:"rps_density_threshold"`

	BlackHoleCostThreshold float64 `mapstructure:"black_hole_cost_threshold"`
	HighTrafficThreshold   float64 `mapstructure:"high_traffic_threshold"`

	ReservationMargin       float64 `mapstructure:"reservation_margin"`
	CPUSafetyFactor         float64 `mapstructure:"cpu_safety_factor"`
	MemorySafetyFactor      float64 `mapstructure:"memory_safety_factor"`
	MaxCollectorConcurrency int `mapstructure:"collector_max_concurrency"`

	TaskServicePatterns []string `mapstructure:"task_service_patterns"`
}

type FinOpsConfig struct {
	CostEngine  CostEngineConfig `mapstructure:"cost_engine"`
	Forecasting ForecastConfig   `mapstructure:"forecasting"`
	Governance  GovernanceConfig `mapstructure:"governance"`
	Monitoring  MonitoringConfig `mapstructure:"monitoring"`
	Reporting   ReportingConfig  `mapstructure:"reporting"`
}

type CostEngineConfig struct {
	Enabled              bool    `mapstructure:"enabled"`
	SafetyMargin         float64 `mapstructure:"safety_margin"`
	OptimizationInterval string  `mapstructure:"optimization_interval"`
}

type ForecastConfig struct {
	Enabled             bool    `mapstructure:"enabled"`
	ModelType           string  `mapstructure:"model_type"`
	PredictionHorizon   int     `mapstructure:"prediction_horizon"`
	ConfidenceThreshold float64 `mapstructure:"confidence_threshold"`
}

type GovernanceConfig struct {
	Enabled  bool     `mapstructure:"enabled"`
	Policies []Policy `mapstructure:"policies"`
}

type Policy struct {
	Name        string  `mapstructure:"name"`
	Type        string  `mapstructure:"type"`
	Scope       string  `mapstructure:"scope"`
	Threshold   float64 `mapstructure:"threshold"`
	Severity    string  `mapstructure:"severity"`
	Action      string  `mapstructure:"action"`
	Description string  `mapstructure:"description"`
}

type MonitoringConfig struct {
	Enabled             bool           `mapstructure:"enabled"`
	CostThreshold       float64        `mapstructure:"cost_threshold"`
	EfficiencyThreshold float64        `mapstructure:"efficiency_threshold"`
	AlertChannels       []AlertChannel `mapstructure:"alert_channels"`
}

type AlertChannel struct {
	Type   string                 `mapstructure:"type"`
	Config map[string]interface{} `mapstructure:"config"`
}

type ReportingConfig struct {
	Enabled         bool     `mapstructure:"enabled"`
	ReportFrequency string   `mapstructure:"report_frequency"`
	Recipients      []string `mapstructure:"recipients"`
}

type GatewaySpec struct {
	Price float64 `mapstructure:"price"`
	Count int     `mapstructure:"count"`
}

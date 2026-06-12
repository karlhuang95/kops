package config

import (
	"fmt"

	"github.com/spf13/viper"
)

func LoadConfig(cfgFile string) (*GlobalConfig, error) {
	v := viper.New()

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.AddConfigPath(".")
		v.SetConfigName("config")
		v.SetConfigType("yaml")
	}

	v.AutomaticEnv()

	v.SetDefault("governance.target_utilization", 0.8)
	v.SetDefault("governance.memory_target_utilization", 0.8)
	v.SetDefault("governance.cpu_step", 50)
	v.SetDefault("governance.memory_step", 128)
	v.SetDefault("governance.min_cpu", 100)
	v.SetDefault("governance.min_memory", 128)
	v.SetDefault("governance.cpu_limit_factor", 2.0)
	v.SetDefault("governance.mem_limit_factor", 1.1)
	v.SetDefault("governance.reservation_margin", 0.15)
	v.SetDefault("governance.p95_low_threshold", 0.2)
	v.SetDefault("governance.cpu_safety_factor", 1.0)
	v.SetDefault("governance.memory_safety_factor", 1.0)
	v.SetDefault("governance.collector_max_concurrency", 15)
	v.SetDefault("governance.task_service_patterns", []string{"cron", "consumer", "job", "worker"})

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	cfg := &GlobalConfig{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func ValidateConfig(cfg *GlobalConfig) error {
	if cfg == nil {
		return fmt.Errorf("配置为空")
	}
	if cfg.Cost.CPUCores <= 0 {
		return fmt.Errorf("配置错误: cost.cpu_cores 必须大于 0")
	}
	if cfg.Cost.MemoryGB <= 0 {
		return fmt.Errorf("配置错误: cost.memory_gb 必须大于 0")
	}
	if cfg.Governance.CPUStep <= 0 {
		return fmt.Errorf("配置错误: governance.cpu_step 必须大于 0")
	}
	if cfg.Governance.MemoryStep <= 0 {
		return fmt.Errorf("配置错误: governance.memory_step 必须大于 0")
	}
	if cfg.Governance.TargetUtilization <= 0 || cfg.Governance.TargetUtilization > 1 {
		return fmt.Errorf("配置错误: governance.target_utilization 必须在 (0,1] 范围内")
	}
	if cfg.Governance.MemoryTargetUtilization <= 0 || cfg.Governance.MemoryTargetUtilization > 1 {
		return fmt.Errorf("配置错误: governance.memory_target_utilization 必须在 (0,1] 范围内")
	}
	if cfg.GatewayCost.Count < 0 {
		return fmt.Errorf("配置错误: gateway_cost.count 不能小于 0")
	}
	return nil
}

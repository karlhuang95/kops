package config

import (
	platformconfig "kops/internal/platform/config"
	"testing"
)

func TestValidateConfig(t *testing.T) {
	valid := &GlobalConfig{
		Cost: CostSpec{
			Price:    1000,
			CPUCores: 16,
			MemoryGB: 64,
		},
		Governance: GovSpec{
			TargetUtilization: 0.8,
			CPUStep:           50,
			MemoryStep:        128,
		},
		GatewayCost: GatewaySpec{
			Count: 2,
		},
	}

	tests := []struct {
		name    string
		cfg     *GlobalConfig
		wantErr bool
	}{
		{name: "valid", cfg: valid, wantErr: false},
		{
			name: "missing cpu cores",
			cfg: &GlobalConfig{
				Cost:       CostSpec{MemoryGB: 64},
				Governance: GovSpec{TargetUtilization: 0.8, CPUStep: 50, MemoryStep: 128},
			},
			wantErr: true,
		},
		{
			name: "invalid utilization",
			cfg: &GlobalConfig{
				Cost:       CostSpec{CPUCores: 16, MemoryGB: 64},
				Governance: GovSpec{TargetUtilization: 1.5, CPUStep: 50, MemoryStep: 128},
			},
			wantErr: true,
		},
		{
			name: "invalid cpu step",
			cfg: &GlobalConfig{
				Cost:       CostSpec{CPUCores: 16, MemoryGB: 64},
				Governance: GovSpec{TargetUtilization: 0.8, CPUStep: 0, MemoryStep: 128},
			},
			wantErr: true,
		},
		{
			name: "invalid gateway count",
			cfg: &GlobalConfig{
				Cost:        CostSpec{CPUCores: 16, MemoryGB: 64},
				Governance:  GovSpec{TargetUtilization: 0.8, CPUStep: 50, MemoryStep: 128},
				GatewayCost: GatewaySpec{Count: -1},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := platformconfig.ValidateConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

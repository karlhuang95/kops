package cmd

import (
	"fmt"

	"kops/pkg/config"
)

func loadCommandConfig(cfgPath string) (*config.GlobalConfig, error) {
	return config.LoadConfig(cfgPath)
}

func printCommandError(label string, err error) {
	fmt.Printf("❌ %s: %v\n", label, err)
}

package config

import platformconfig "kops/internal/platform/config"

func LoadConfig(cfgFile string) (*GlobalConfig, error) {
	return platformconfig.LoadConfig(cfgFile)
}

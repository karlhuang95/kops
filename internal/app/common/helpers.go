package common

import (
	"fmt"
	"os"
	"sort"

	healthdomain "kops/internal/domain/health"
	platformconfig "kops/internal/platform/config"
)

func ResolveTargetNamespaces(cfg *platformconfig.GlobalConfig, namespace string) []string {
	if namespace != "" {
		return []string{namespace}
	}
	return cfg.Namespaces
}

func SortHealthStatuses(results []healthdomain.HealthStatus) {
	priorityOrder := map[string]int{"Critical": 0, "Warning": 1, "Healthy": 2, "Idle": 3}
	sort.Slice(results, func(i, j int) bool {
		return priorityOrder[results[i].HealthCode] < priorityOrder[results[j].HealthCode]
	})
}

func SaveToFile(filename, content string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer file.Close()

	if _, err := file.WriteString(content); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	return nil
}

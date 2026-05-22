package cmd

import (
	"fmt"

	appanalyze "kops/internal/app/analyze"

	"github.com/spf13/cobra"
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Kubernetes 资源、效率与健康一体化分析",
	Long: `kops analyze 采集一次 Prometheus 指标，同时完成资源建议、流量效率、服务健康三类分析，
合并输出为统一报告。

分析维度：
1. 资源推荐：基于 P95 利用率推荐 CPU/内存 Request，核算成本与风险
2. 流量效率：计算流量密度分级 (S/A/B/C)，识别资源黑洞
3. 健康状态：基于 Traefik 指标判定服务健康（Critical/Warning/Healthy/Idle）

输出格式：table（默认）、csv、json、markdown/md`,
	Run: func(cmd *cobra.Command, args []string) {
		cfgPath, _ := cmd.Flags().GetString("config")
		namespace, _ := cmd.Flags().GetString("namespace")
		outputFormat, _ := cmd.Flags().GetString("output")
		durationStr, _ := cmd.Flags().GetString("duration")
		thresholdStr, _ := cmd.Flags().GetString("threshold")

		cfg, err := loadCommandConfig(cfgPath)
		if err != nil {
			printCommandError("无法读取配置文件", err)
			return
		}

		report, err := appanalyze.Run(appanalyze.AnalyzeParams{
			Config:       cfg,
			Namespace:    namespace,
			OutputFormat: outputFormat,
			Duration:     durationStr,
			Threshold5xx: thresholdStr,
		})
		if err != nil {
			printCommandError("Analyze 执行失败", err)
			return
		}
		if report != "" {
			fmt.Print(report)
		}
	},
}

func init() {
	rootCmd.AddCommand(analyzeCmd)

	analyzeCmd.Flags().StringP("namespace", "n", "", "指定要分析的命名空间 (默认使用配置文件中的列表)")
	analyzeCmd.Flags().StringP("output", "o", "table", "报告输出格式 (支持 table, csv, json, markdown/md)")
	analyzeCmd.Flags().StringP("duration", "d", "5m", "健康检查分析时间窗口")
	analyzeCmd.Flags().StringP("threshold", "t", "0.02", "5xx 错误率报警阈值 (0.02 = 2%)")
}

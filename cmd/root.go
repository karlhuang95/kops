/*
Copyright © 2026 Gemini Advisor Tools <karlhuang93@gmail.com>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var cfgFile string

// rootCmd 代表基础命令，不带子命令直接调用时触发
var rootCmd = &cobra.Command{
	Use:   "kops",
	Short: "K8s 资源画像分析与成本优化专家工具",
	Long: `kops (Kubernetes Operations & Optimization) 是一个专业的 K8s 治理工具。
它通过深度分析 Prometheus 监控数据，结合业务自定义的经济核算模型，
为运维和开发团队提供科学的资源规格建议、容量评估以及成本节约报告。

核心能力：
- Analyze: 一体化分析，采集一次数据同时输出资源建议、效率分析与健康检查。
- Profile: 查看指定 Deployment 的历史流量与资源消耗曲线。`,
	// 如果你希望直接运行 kops 时显示帮助信息，可以不写 Run 函数
}

// Execute 将所有子命令添加到根命令并设置相应的标志。
// 这是由 main.main() 调用的。它只需要对 rootCmd 发生一次。
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// 定义全局持久化 Flag，所有子命令都能使用
	// 例如指定配置文件路径，默认读取当前目录下的 config.yaml
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "config.yaml", "配置文件路径 (default: ./config.yaml)")

	// 本地 Flag，仅在直接调用 kops 时生效
	rootCmd.Flags().BoolP("version", "v", false, "显示版本信息")
}

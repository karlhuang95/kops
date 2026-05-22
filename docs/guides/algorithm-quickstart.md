# 算法相关命令

这份文档只保留和算法验证直接相关的命令。

## 统一分析

```bash
./kops analyze --config config.yaml
./kops analyze --config config.yaml -o markdown
./kops analyze --config config.yaml -o csv
./kops analyze --config config.yaml -o json
```

适合验证：
- 资源建议和成本分摊
- Gateway 成本分摊（按流量权重）
- 黑洞和核心流量服务判定
- 流量密度和浪费金额
- 健康码和错误率损耗
- 全链路无效投入

## 测试

```bash
go test ./...
go test ./pkg/algorithm/... -v
go test ./pkg/advisor/... -v
go test ./internal/platform/pricing/... -v
```

package model

import advisordomain "kops/internal/domain/advisor"
import healthdomain "kops/internal/domain/health"
import metricsdomain "kops/internal/domain/metrics"

type ServiceMetrics = metricsdomain.ServiceMetrics

type AdviceResult = advisordomain.AdviceResult
type EfficiencyResult = advisordomain.EfficiencyResult
type ResourceBlackHole = advisordomain.ResourceBlackHole

type HealthMetrics = healthdomain.HealthMetrics
type HealthStatus = healthdomain.HealthStatus

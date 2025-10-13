package health

import (
	"liuproxy_gateway/internal/shared/logger"
	"liuproxy_gateway/internal/shared/types"
	"sync"
	"time"
)

// Checker 负责对策略实例进行健康检查。
type Checker struct {
	isMobileMode bool
}

// New 创建一个新的 Checker 实例。
func New(isMobile bool) *Checker {
	return &Checker{
		isMobileMode: isMobile,
	}
}

// Check 对传入的策略实例 map 进行并发健康检查。
// 它会根据 Checker 的模式（移动/PC）选择不同的检查方法。
func (c *Checker) Check(instancesToCheck map[string]types.TunnelStrategy) (map[string]types.HealthStatus, map[string]*types.Metrics, map[string]string) {
	healthStatus := make(map[string]types.HealthStatus)
	metricsCache := make(map[string]*types.Metrics)
	exitIPs := make(map[string]string)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for id, strat := range instancesToCheck {
		wg.Add(1)
		go func(serverID string, st types.TunnelStrategy) {
			defer wg.Done()

			metrics := st.GetMetrics()
			var currentHealth types.HealthStatus
			var currentExitIP string
			var latency int64
			var err error

			logFields := logger.Debug().Str("server_id", serverID).Str("strategy_type", st.GetType())

			if c.isMobileMode {
				// 移动模式: 强制调用基础方法
				logFields.Msg("HealthCheck: Mobile mode, using basic check.")
				start := time.Now()
				err = st.CheckHealth()
				latency = time.Since(start).Milliseconds()
			} else {
				// PC 模式: 优先尝试高级方法
				if adv, ok := st.(types.AdvancedHealthChecker); ok {
					logFields.Msg("HealthCheck: PC mode, using advanced check.")
					latency, currentExitIP, err = adv.CheckHealthAdvanced()
				} else {
					// 回退到基础方法
					logFields.Msg("HealthCheck: PC mode, advanced not supported, using basic check (fallback).")
					start := time.Now()
					err = st.CheckHealth()
					latency = time.Since(start).Milliseconds()
				}
			}

			metrics.Latency = latency

			if err == nil {
				currentHealth = types.StatusUp
				logFields.Bool("success", true).Int64("latency_ms", metrics.Latency).Str("exit_ip", currentExitIP).Msg("HealthCheck: Check passed.")
			} else {
				currentHealth = types.StatusDown
				logFields.Bool("success", false).Err(err).Str("dial_error", err.Error()).Msg("HealthCheck: Check failed with specific error.")
			}

			mu.Lock()
			healthStatus[serverID] = currentHealth
			metricsCache[serverID] = metrics
			exitIPs[serverID] = currentExitIP
			mu.Unlock()
		}(id, strat)
	}

	wg.Wait()
	return healthStatus, metricsCache, exitIPs
}

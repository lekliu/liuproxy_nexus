package dispatcher

import (
	"context"
	"fmt"
	"github.com/rs/zerolog/log"
	"liuproxy_nexus/internal/shared/settings"
	"liuproxy_nexus/internal/shared/types"
	"math"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

const maxRecentTargets = 20 // 定义历史记录的最大数量

// RouteInfo 存储了路由决策所需的目标信息。
type RouteInfo struct {
	ServerID string
	Strategy types.TunnelStrategy // <-- 持有 TunnelStrategy 实例
}

// processedRule 将解析后的路由信息与原始规则绑定，并用于排序。
type processedRule struct {
	rule  *settings.Rule
	route *RouteInfo
}

// --- Load Balancer Strategy Pattern ---

// LoadBalancer defines the interface for backend selection strategies.
type LoadBalancer interface {
	Select(serverStates map[string]*types.ServerState) (*types.ServerState, error)
}

// LeastConnectionsBalancer selects the backend with the minimum number of active connections.
type LeastConnectionsBalancer struct{}

func (b *LeastConnectionsBalancer) Select(serverStates map[string]*types.ServerState) (*types.ServerState, error) {
	var bestServer *types.ServerState
	minConnections := int64(math.MaxInt64)

	for _, state := range serverStates {
		if state.Profile.Active && state.Health == types.StatusUp {
			if state.Instance != nil && state.Metrics != nil {
				if state.Metrics.ActiveConnections < minConnections {
					minConnections = state.Metrics.ActiveConnections
					bestServer = state
				}
			}
		}
	}

	if bestServer == nil {
		return nil, fmt.Errorf("no healthy backends available")
	}
	return bestServer, nil
}

// RoundRobinBalancer selects backends in a sequential order.
type RoundRobinBalancer struct {
	next uint32
}

func NewRoundRobinBalancer() *RoundRobinBalancer {
	return &RoundRobinBalancer{}
}

func (b *RoundRobinBalancer) Select(serverStates map[string]*types.ServerState) (*types.ServerState, error) {
	// On-the-fly creation of the available server list ensures it's always up-to-date.
	availableIDs := make([]string, 0)
	for id, state := range serverStates {
		if state.Profile.Active && state.Health == types.StatusUp {
			availableIDs = append(availableIDs, id)
		}
	}

	if len(availableIDs) == 0 {
		return nil, fmt.Errorf("no healthy backends available for round robin")
	}

	// Sort the IDs to ensure a consistent round-robin order between calls
	sort.Strings(availableIDs)

	nextIndex := atomic.AddUint32(&b.next, 1) - 1
	selectedID := availableIDs[nextIndex%uint32(len(availableIDs))]

	return serverStates[selectedID], nil
}

// Dispatcher 实现了 types.Dispatcher 接口，是路由和负载均衡的核心。
// 它现在也实现了 settings.ConfigurableModule 接口，使其配置可以被热重载。
type Dispatcher struct {
	stateProvider types.StateProvider
	strategyMutex sync.RWMutex

	// 使用一个单一的、预排序的规则列表
	sortedRules []*processedRule

	// 使用 atomic.Value 来原子地存储和替换 StickyManager 实例，实现无锁读取和热重载
	stickyManager atomic.Value
	// 使用 atomic.Value 来存储和切换负载均衡策略
	loadBalancer atomic.Value

	recentTargetsMutex sync.Mutex
	recentTargets      []string // 用于存储最近的目标地址
}

// New 创建一个新的 Dispatcher 实例。
func New(
	initialGatewaySettings *settings.GatewaySettings,
	stateProvider types.StateProvider,
) *Dispatcher {
	d := &Dispatcher{
		stateProvider: stateProvider,
		sortedRules:   make([]*processedRule, 0),
		recentTargets: make([]string, 0, maxRecentTargets),
	}

	// 基于初始配置创建第一个 StickyManager
	initialStickyManager := NewStickyManager(initialGatewaySettings)
	d.stickyManager.Store(initialStickyManager)

	d.updateLoadBalancer(initialGatewaySettings.LoadBalancerStrategy)

	return d
}

// getStickyManager 是一个线程安全的辅助函数，用于获取当前最新的 StickyManager 实例。
func (d *Dispatcher) getStickyManager() *StickyManager {
	return d.stickyManager.Load().(*StickyManager)
}

func (d *Dispatcher) updateLoadBalancer(strategy string) {
	log.Debug().Str("strategy", strategy).Msg("Updating load balancer strategy.")
	var lb LoadBalancer
	switch strategy {
	case "round_robin":
		lb = NewRoundRobinBalancer()
	case "least_connections":
		fallthrough
	default:
		lb = &LeastConnectionsBalancer{}
	}
	d.loadBalancer.Store(lb)
}

// OnSettingsUpdate 实现了 settings.ConfigurableModule 接口。
// 当网关设置发生变化时，此方法将被 SettingsManager 调用。
func (d *Dispatcher) OnSettingsUpdate(moduleKey string, newSettings interface{}) error {
	switch moduleKey {
	case "gateway":
		cfg, ok := newSettings.(*settings.GatewaySettings)
		if !ok {
			return fmt.Errorf("dispatcher: received incorrect settings type for gateway module")
		}

		// Stop the old StickyManager's background tasks
		oldManager := d.getStickyManager()
		oldManager.Stop()

		// Create a brand new StickyManager instance
		newStickyManager := NewStickyManager(cfg)
		newStickyManager.Start() // Start the new background tasks

		// Atomically replace the old instance
		d.stickyManager.Store(newStickyManager)

		// Update load balancer strategy
		d.updateLoadBalancer(cfg.LoadBalancerStrategy)

	case "routing":
		cfg, ok := newSettings.(*settings.RoutingSettings)
		if !ok {
			return fmt.Errorf("dispatcher: received incorrect settings type for routing module")
		}
		d.updateRoutingTables(cfg)
	}
	return nil
}

// Start 启动 Dispatcher 的后台任务 (如粘性会话清理)。
func (d *Dispatcher) Start() {
	d.getStickyManager().Start()
}

// Stop 停止 Dispatcher 的后台任务。
func (d *Dispatcher) Stop() {
	d.getStickyManager().Stop()
}

// Dispatch 是路由决策的核心入口。
func (d *Dispatcher) Dispatch(ctx context.Context, source net.Addr, target string) (types.TunnelStrategy, string, error) {
	d.recordTarget(target)
	clientIPStr, _, _ := net.SplitHostPort(source.String())
	targetHost, _, _ := net.SplitHostPort(target)
	clientIP, err := netip.ParseAddr(clientIPStr)
	if err != nil {
		return nil, "", fmt.Errorf("invalid source IP: %s", clientIPStr)
	}

	d.getStickyManager().RecordClientActivity(clientIPStr)

	// 从 stateProvider 实时获取当前状态
	serverStates := d.stateProvider.GetServerStates()

	d.strategyMutex.RLock()
	rules := d.sortedRules
	d.strategyMutex.RUnlock()

	// 1. 遍历排序后的规则列表进行匹配
	for _, pRule := range rules {
		rule := pRule.rule
		route := pRule.route

		var matched bool
		var matchedValue string

		switch rule.Type {
		case string(settings.RuleTypeDomain):
			domainLower := strings.ToLower(targetHost)
			for _, pattern := range rule.Value {
				pLower := strings.ToLower(pattern)
				// 规则以 '.' 开头 (e.g., .baidu.com), 仅匹配子域名
				if strings.HasPrefix(pLower, ".") {
					if strings.HasSuffix(domainLower, pLower) {
						matched = true
					}
				} else { // 规则不以 '.' 开头 (e.g., baidu.com), 匹配自身和所有子域名
					if domainLower == pLower || strings.HasSuffix(domainLower, "."+pLower) {
						matched = true
					}
				}
				if matched {
					matchedValue = pattern
					break
				}
			}
		case string(settings.RuleTypeSourceIP):
			for _, val := range rule.Value {
				cidr := val
				if !strings.Contains(cidr, "/") {
					if ip := net.ParseIP(cidr); ip != nil {
						if ip.To4() != nil {
							cidr += "/32"
						} else {
							cidr += "/128"
						}
					}
				}

				prefix, err := netip.ParsePrefix(cidr)
				if err == nil && prefix.Contains(clientIP) {
					matched = true
					matchedValue = cidr
					break
				}
			}
		case string(settings.RuleTypeDestIP):
			var targetIP netip.Addr
			var parseErr error

			if targetIP, parseErr = netip.ParseAddr(targetHost); parseErr != nil {
				ips, lookupErr := net.LookupIP(targetHost)
				if lookupErr == nil {
					for _, ip := range ips {
						addr, ok := netip.AddrFromSlice(ip)
						if ok {
							targetIP = addr
							break // Use the first resolved IP
						}
					}
				}
			}

			if targetIP.IsValid() {
				for _, cidr := range rule.Value {
					prefix, err := netip.ParsePrefix(cidr)
					if err == nil && prefix.Contains(targetIP) {
						matched = true
						matchedValue = cidr
						break
					}
				}
			}
		}

		if matched {
			log.Ctx(ctx).Debug().
				Int("priority", rule.Priority).
				Str("type", rule.Type).
				Str("value", matchedValue).
				Str("target", rule.Target).
				Msg("Dispatcher: Matched routing rule.")

			// 对于 DIRECT/REJECT, Strategy 为 nil
			if route.Strategy == nil {
				return nil, route.ServerID, nil
			}

			if serverState, ok := serverStates[route.ServerID]; !ok || !serverState.Profile.Active || serverState.Health != types.StatusUp {
				log.Ctx(ctx).Warn().Str("target_id", route.ServerID).Msg("Dispatcher: Matched rule's backend is not active or healthy. Continuing search...")
				continue
			}
			return route.Strategy, route.ServerID, nil
		}
	}

	sm := d.getStickyManager()
	sm_ShouldApply := sm.ShouldApply(targetHost)
	if sm_ShouldApply {
		stickyKey := clientIPStr + ":" + targetHost
		if record := sm.Get(stickyKey, serverStates); record != nil {
			if serverState, ok := serverStates[record.ServerID]; ok && serverState.Instance != nil {
				// The instance listener info is now inside the ServerState
				log.Ctx(ctx).Debug().
					Str("client_ip", clientIPStr).
					Str("target_host", targetHost).
					Str("matched_by", "Sticky Session").
					Str("server_id", record.ServerID).
					Msg("Dispatcher: Sticky route dispatched using live port.")
				return serverState.Instance, record.ServerID, nil
			}
			log.Ctx(ctx).Debug().
				Str("server_id", record.ServerID).
				Msg("Dispatcher: Sticky session record found but server is no longer active. Falling back to load balancer.")
		}
	}

	// 2. 执行负载均衡
	chosenStrategy, chosenServerID, err := d.GetBackendForLoadBalancing(serverStates)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("Dispatcher: Load Balancer found no healthy backends.")
		return nil, "", fmt.Errorf("no route matched for target '%s' and no healthy backends available", target)
	}

	// 3. 如果需要，将新选择的后端存入粘性缓存
	if sm_ShouldApply {
		stickyKey := clientIPStr + ":" + targetHost
		sm.Set(stickyKey, chosenServerID)
	}

	log.Ctx(ctx).Debug().
		Str("client_ip", clientIPStr).
		Str("target_host", targetHost).
		Str("matched_by", "Load Balancer").
		Msg("Dispatcher: Load balanced route dispatched.")
	return chosenStrategy, chosenServerID, nil
}

func (d *Dispatcher) recordTarget(target string) {
	d.recentTargetsMutex.Lock()
	defer d.recentTargetsMutex.Unlock()

	// 检查目标是否已存在，如果存在则不重复添加
	for _, t := range d.recentTargets {
		if t == target {
			return
		}
	}

	// 如果列表已满，移除最旧的条目
	if len(d.recentTargets) >= maxRecentTargets {
		d.recentTargets = d.recentTargets[1:]
	}

	// 添加新条目到末尾
	d.recentTargets = append(d.recentTargets, target)
}

// --- GetRecentTargets ---
func (d *Dispatcher) GetRecentTargets(rules []*settings.Rule) []string {
	d.recentTargetsMutex.Lock()
	// Create a copy of recent targets to work with, to minimize lock time
	targetsCopy := make([]string, len(d.recentTargets))
	copy(targetsCopy, d.recentTargets)
	d.recentTargetsMutex.Unlock()

	configuredValues := make(map[string]struct{})
	for _, rule := range rules {
		for _, val := range rule.Value {
			configuredValues[val] = struct{}{}
		}
	}

	uniqueTargets := make(map[string]struct{})
	for _, target := range targetsCopy {
		host, _, err := net.SplitHostPort(target)
		if err != nil {
			host = target // Assume it's already a host
		}

		// Filter if the host is already in a rule
		if _, exists := configuredValues[host]; !exists {
			uniqueTargets[host] = struct{}{}
		}
	}

	result := make([]string, 0, len(uniqueTargets))
	for target := range uniqueTargets {
		result = append(result, target)
	}
	return result
}

// UpdateRoutingTables rebuilds the internal routing tables based on a provided list of compiled rules.
// It is called by AppServer after all dynamic rules have been processed.
func (d *Dispatcher) updateRoutingTables(cfg *settings.RoutingSettings) {
	d.strategyMutex.Lock()
	defer d.strategyMutex.Unlock()

	log.Debug().Msg("Dispatcher: Rebuilding routing tables based on new settings...")

	serverStates := d.stateProvider.GetServerStates()
	allProcessedRules := make([]*processedRule, 0, len(cfg.Rules))

	for _, rule := range cfg.Rules {
		routeInfo := &RouteInfo{}
		if rule.Target == "DIRECT" || rule.Target == "REJECT" {
			routeInfo.ServerID = rule.Target
			routeInfo.Strategy = nil // Strategy is nil
		} else {
			var targetState *types.ServerState
			for _, state := range serverStates {
				if state.Profile.Remarks == rule.Target {
					targetState = state
					break
				}
			}

			if targetState == nil {
				log.Warn().Str("target_remarks", rule.Target).Msg("Routing rule target not found, skipping rule.")
				continue
			}

			// A static route target must be active and healthy to be included
			if targetState.Profile.Active && targetState.Health == types.StatusUp && targetState.Instance != nil {
				routeInfo.ServerID = targetState.Profile.ID
				routeInfo.Strategy = targetState.Instance // 存储实例引用

			} else {
				log.Warn().Str("target_remarks", rule.Target).Msg("Routing rule target is not active or not healthy, skipping rule.")
				continue
			}
		}

		allProcessedRules = append(allProcessedRules, &processedRule{
			rule:  rule,
			route: routeInfo,
		})
	}

	// 根据优先级排序，值越小越优先
	sort.Slice(allProcessedRules, func(i, j int) bool {
		return allProcessedRules[i].rule.Priority < allProcessedRules[j].rule.Priority
	})

	d.sortedRules = allProcessedRules

	log.Debug().Int("rule_count", len(d.sortedRules)).Msg("Dispatcher: Routing tables updated successfully.")
}

// ReportFailure 现在使用内部的 failureReporter 字段。
func (d *Dispatcher) ReportFailure(serverID string) {
	log.Debug().Str("server_id", serverID).Msg("Dispatcher: ReportFailure called. Passing to failure reporter.")
	if serverID == "DIRECT" || serverID == "REJECT" {
		return
	}

}

// ReportSuccess 现在使用内部的 failureReporter 字段。
func (d *Dispatcher) ReportSuccess(serverID string) {
	if serverID == "DIRECT" || serverID == "REJECT" {
		return
	}
}

// GetBackendForLoadBalancing 从健康的激活策略池中选择一个后端。
func (d *Dispatcher) GetBackendForLoadBalancing(serverStates map[string]*types.ServerState) (types.TunnelStrategy, string, error) {
	// 修正: 直接将 atomic.Value 的值断言为接口类型 `LoadBalancer`
	// 而不是错误的 `*LoadBalancer` (指向接口的指针)
	lb := d.loadBalancer.Load().(LoadBalancer)
	chosenServer, err := lb.Select(serverStates)
	if err != nil {
		return nil, "", err
	}

	if chosenServer.Instance == nil {
		return nil, "", fmt.Errorf("load balancer selected an inactive server instance for ID %s", chosenServer.Profile.ID)
	}

	return chosenServer.Instance, chosenServer.Profile.ID, nil
}

// GetRecentClientIPs 从粘性会话管理器中获取最近活跃的客户端IP列表。
func (d *Dispatcher) GetRecentClientIPs() []string {
	sm := d.getStickyManager()
	return sm.GetAllClientIPs()
}

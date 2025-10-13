// FILE: internal/firewall/engine.go
package firewall

import (
	"fmt"
	"liuproxy_gateway/internal/shared/logger"
	"liuproxy_gateway/internal/shared/settings"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Firewall 接口定义了防火墙引擎的行为
type Firewall interface {
	Check(metadata *ConnectionMetadata) settings.FirewallAction
	settings.ConfigurableModule
}

// ConnectionMetadata 包含了防火墙决策所需的信息
type ConnectionMetadata struct {
	Protocol    string // "tcp" or "udp"
	Source      net.Addr
	Destination net.Addr
}

type portRange struct {
	start, end uint16
}

type parsedFirewallRule struct {
	original   *settings.FirewallRule
	sourceNets []*net.IPNet
	destNets   []*net.IPNet
	portRanges []portRange
}

// Engine 实现了 Firewall 接口
type Engine struct {
	mu      sync.RWMutex
	rules   []*parsedFirewallRule
	enabled bool
}

// NewEngine 创建一个新的防火墙引擎实例
func NewEngine() *Engine {
	return &Engine{}
}

// OnSettingsUpdate 实现了 settings.ConfigurableModule 接口，用于热重载规则
func (e *Engine) OnSettingsUpdate(moduleKey string, newSettings interface{}) error {
	if moduleKey != "firewall" {
		return nil
	}
	cfg, ok := newSettings.(*settings.FirewallSettings)
	if !ok {
		return fmt.Errorf("firewall: received incorrect settings type")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.enabled = cfg.Enabled
	if !e.enabled {
		logger.Info().Msg("Firewall is disabled by configuration. All traffic will be allowed by default.")
		e.rules = nil // 清空规则
		return nil
	}

	validRules := make([]*parsedFirewallRule, 0, len(cfg.Rules))
	for _, rule := range cfg.Rules {
		pr, err := parseRule(rule)
		if err != nil {
			logger.Error().Err(err).Interface("rule", rule).Msg("Failed to parse firewall rule, skipping.")
			continue
		}
		validRules = append(validRules, pr)
	}

	// 按优先级排序
	sort.SliceStable(validRules, func(i, j int) bool {
		return validRules[i].original.Priority < validRules[j].original.Priority
	})

	e.rules = validRules
	logger.Info().Int("count", len(e.rules)).Msg("Firewall rules updated successfully.")
	return nil
}

// Check 将根据加载的规则对连接进行检查
func (e *Engine) Check(meta *ConnectionMetadata) settings.FirewallAction {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.enabled || len(e.rules) == 0 {
		return settings.ActionAllow // 如果防火墙禁用或无规则，则默认允许
	}

	// 从 net.Addr 中提取 IP 和端口
	var srcIP, destIP net.IP
	var destPort uint16

	if tcpDest, ok := meta.Destination.(*net.TCPAddr); ok {
		destIP = tcpDest.IP
		destPort = uint16(tcpDest.Port)
	} else if udpDest, ok := meta.Destination.(*net.UDPAddr); ok {
		destIP = udpDest.IP
		destPort = uint16(udpDest.Port)
	} else {
		return settings.ActionAllow // 无法解析目标地址，默认放行
	}

	if tcpSrc, ok := meta.Source.(*net.TCPAddr); ok {
		srcIP = tcpSrc.IP
	} else if udpSrc, ok := meta.Source.(*net.UDPAddr); ok {
		srcIP = udpSrc.IP
	} else {
		return settings.ActionAllow // 无法解析源地址，默认放行
	}

	for _, rule := range e.rules {
		if rule.matches(meta.Protocol, srcIP, destIP, destPort) {
			// 【调试日志】
			logger.Debug().
				Str("action", string(rule.original.Action)).
				Int("priority", rule.original.Priority).
				Str("src", srcIP.String()).
				Str("dest", destIP.String()+":"+strconv.Itoa(int(destPort))).
				Msg("Firewall rule matched.")
			return rule.original.Action
		}
	}

	// 【调试日志】如果没有任何规则匹配，也应该有日志
	logger.Debug().
		Str("action", string(settings.ActionDeny)).
		Str("reason", "No rule matched, default deny").
		Str("src", srcIP.String()).
		Str("dest", destIP.String()+":"+strconv.Itoa(int(destPort))).
		Msg("Firewall check finished.")

	// 安全默认：如果没有规则匹配，则拒绝
	return settings.ActionDeny
}

func (pr *parsedFirewallRule) matches(proto string, srcIP, destIP net.IP, destPort uint16) bool {
	// 协议匹配
	if pr.original.Protocol != "" && !strings.EqualFold(pr.original.Protocol, proto) {
		return false
	}
	// 源 IP 匹配
	if len(pr.sourceNets) > 0 {
		match := false
		for _, network := range pr.sourceNets {
			if network.Contains(srcIP) {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	// 目标 IP 匹配
	if len(pr.destNets) > 0 {
		match := false
		for _, network := range pr.destNets {
			if network.Contains(destIP) {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	// 目标端口匹配
	if len(pr.portRanges) > 0 {
		match := false
		for _, prange := range pr.portRanges {
			if destPort >= prange.start && destPort <= prange.end {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	return true
}

func parseRule(rule *settings.FirewallRule) (*parsedFirewallRule, error) {
	pr := &parsedFirewallRule{original: rule}
	var err error

	pr.sourceNets, err = parseCIDRs(rule.SourceCIDR)
	if err != nil {
		return nil, err
	}

	pr.destNets, err = parseCIDRs(rule.DestCIDR)
	if err != nil {
		return nil, err
	}

	pr.portRanges, err = parsePortRanges(rule.DestPort)
	if err != nil {
		return nil, err
	}

	return pr, nil
}

func parseCIDRs(cidrs []string) ([]*net.IPNet, error) {
	if len(cidrs) == 0 {
		return nil, nil
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidrStr := range cidrs {
		trimmedCidr := strings.TrimSpace(cidrStr)
		if trimmedCidr == "" {
			continue
		}

		// 检查是否为单个 IP 地址
		if !strings.Contains(trimmedCidr, "/") {
			ip := net.ParseIP(trimmedCidr)
			if ip == nil {
				return nil, fmt.Errorf("invalid IP address format: '%s'", trimmedCidr)
			}
			// 如果是单个IP，为其添加正确的掩码
			if ip.To4() != nil {
				trimmedCidr += "/32" // IPv4
			} else {
				trimmedCidr += "/128" // IPv6
			}
		}

		_, network, err := net.ParseCIDR(trimmedCidr)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR '%s': %w", trimmedCidr, err)
		}
		nets = append(nets, network)
	}
	return nets, nil
}

func parsePortRanges(portStr string) ([]portRange, error) {
	if portStr == "" {
		return nil, nil
	}
	var ranges []portRange
	parts := strings.Split(portStr, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid port range: %s", part)
			}
			start, err1 := strconv.ParseUint(rangeParts[0], 10, 16)
			end, err2 := strconv.ParseUint(rangeParts[1], 10, 16)
			if err1 != nil || err2 != nil || start > end || start == 0 {
				return nil, fmt.Errorf("invalid port range values: %s", part)
			}
			ranges = append(ranges, portRange{uint16(start), uint16(end)})
		} else {
			port, err := strconv.ParseUint(part, 10, 16)
			if err != nil || port == 0 {
				return nil, fmt.Errorf("invalid port: %s", part)
			}
			ranges = append(ranges, portRange{uint16(port), uint16(port)})
		}
	}
	return ranges, nil
}

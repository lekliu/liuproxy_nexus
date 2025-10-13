package settings

// RuleType 定义了路由规则的类型
type RuleType string

const (
	RuleTypeSourceIP    RuleType = "source_ip"
	RuleTypeDestIP      RuleType = "dest_ip"
	RuleTypeDomain      RuleType = "domain"
	RuleTypeLoadBalance RuleType = "loadbalance" // 特殊类型，代表默认负载均衡
)

// 新增防火墙 Action 常量
type FirewallAction string

const (
	ActionAllow FirewallAction = "allow"
	ActionDeny  FirewallAction = "deny"
)

// 新增防火墙规则结构
type FirewallRule struct {
	Priority   int            `json:"priority"`
	Protocol   string         `json:"protocol,omitempty"` // "tcp", "udp", or empty for both
	SourceCIDR []string       `json:"source_cidr,omitempty"`
	DestCIDR   []string       `json:"dest_cidr,omitempty"`
	DestPort   string         `json:"dest_port,omitempty"` // e.g., "80,443,1000-2000"
	Action     FirewallAction `json:"action"`              // "allow" or "deny"
}

// 新增防火墙设置结构
type FirewallSettings struct {
	Enabled bool            `json:"enabled"`
	Rules   []*FirewallRule `json:"rules"`
}

// ConfigurableModule 是所有希望其配置能被在线管理的模块必须实现的接口。
// 它定义了一个标准的回调方法，当相关配置发生变更时，SettingsManager会调用此方法。
type ConfigurableModule interface {
	// OnSettingsUpdate 在配置变更时被 SettingsManager 调用。
	// moduleKey: 告知是哪个模块的配置发生了变化 (e.g., "gateway", "routing")。
	// newSettings: 是对应模块的、已经解析好的新配置结构体指针 (e.g., *GatewaySettings)。
	OnSettingsUpdate(moduleKey string, newSettings interface{}) error
}

// RuntimeSettings 是 settings.json 文件的顶层结构。
// 它以模块化的方式组织了所有可以在运行时被动态修改的配置。
// 使用指针类型确保了当JSON文件中缺少某个模块时，对应的字段为nil，而不是一个空的结构体。
type RuntimeSettings struct {
	Gateway  *GatewaySettings  `json:"gateway"`
	Routing  *RoutingSettings  `json:"routing"`
	Logging  *LoggingSettings  `json:"logging"`
	Firewall *FirewallSettings `json:"firewall"`
}

// GatewaySettings 对应 settings.json 中的 "gateway" 模块。
type GatewaySettings struct {
	StickySessionMode    string   `json:"sticky_session_mode"`    // e.g., "disabled", "global", "conditional"
	StickySessionTTL     int      `json:"sticky_session_ttl"`     // in seconds
	StickyRules          []string `json:"sticky_rules"`           // list of domains for conditional mode
	LoadBalancerStrategy string   `json:"load_balancer_strategy"` // e.g., "least_connections", "round_robin"
}

type Rule struct {
	Priority int      `json:"priority"`        // Lower value means higher priority
	Type     string   `json:"type"`            // e.g., "domain", "source_ip"
	Value    []string `json:"value,omitempty"` // e.g., ["*.google.com"], ["192.168.1.0/24", "10.0.0.0/8"]
	Target   string   `json:"target"`          // Server remarks, or "DIRECT", "REJECT"
}

// RoutingSettings 对应 settings.json 中的 "routing" 模块。
type RoutingSettings struct {
	Rules []*Rule `json:"rules"` // 包含所有路由规则的列表
}

// LoadBalancerSettings 对应 settings.json 中的 "load_balancer" 模块 (占位符)。
type LoadBalancerSettings struct {
	// TODO: 在迭代 4.2 中具体实现
}

// LoggingSettings 对应 settings.json 中的 "logging" 模块 (占位符)。
type LoggingSettings struct {
	// TODO: 在未来迭代中具体实现
}

func createDefaultSettings() *RuntimeSettings {
	return &RuntimeSettings{
		Gateway: &GatewaySettings{StickySessionMode: "disabled", StickySessionTTL: 300, StickyRules: []string{}},
		Routing: &RoutingSettings{Rules: []*Rule{}},
		Logging: &LoggingSettings{},
		Firewall: &FirewallSettings{
			Enabled: false,
			Rules: []*FirewallRule{
				{Priority: 9999, Action: "allow"},
			},
		},
	}
}

func ensureDefaultModules(s *RuntimeSettings) {
	if s.Gateway == nil {
		s.Gateway = &GatewaySettings{}
	}
	if s.Routing == nil {
		s.Routing = &RoutingSettings{Rules: []*Rule{}}
	}
	if s.Logging == nil {
		s.Logging = &LoggingSettings{}
	}
	if s.Firewall == nil {
		s.Firewall = &FirewallSettings{Rules: []*FirewallRule{}}
	}
}

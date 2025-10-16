package types

import (
	"context"
	"liuproxy_nexus/proxypool/model"
	"net"
)

// Protocol 类型从 gateway 包移动到此处，成为共享类型
type Protocol string

const (
	ProtoSOCKS5  Protocol = "SOCKS5"
	ProtoHTTP    Protocol = "HTTP"
	ProtoTLS     Protocol = "TLS"
	ProtoUnknown Protocol = "UNKNOWN"
)

// TrafficStats 用于报告流量统计信息
type TrafficStats struct {
	Uplink   uint64
	Downlink uint64
}

// UDPPacket 定义了在模块间传递的 UDP 数据包结构
type UDPPacket struct {
	Source      net.Addr
	Destination net.Addr
	Payload     []byte
}

// TunnelBuilder 接口定义了一个可以提供SOCKS5连接的实体。
// 这是 Gateway 和 Strategy 之间新的、解耦的交互方式。
type TunnelBuilder interface {
	GetSocksConnection() (net.Conn, error)
}

// StateManager 定义了一个可以直接修改服务器健康状态的接口
type StateManager interface {
	SetServerStatusDown(serverID, reason string)
}

// AdvancedHealthChecker 是一个可选接口，策略可以实现它以提供更丰富的健康检查信息。
type AdvancedHealthChecker interface {
	CheckHealthAdvanced() (latency int64, exitIP string, err error)
}

// TunnelStrategy 定义了所有策略的通用接口。
type TunnelStrategy interface {
	TunnelBuilder // 嵌入接口，要求所有策略都能提供一个SOCKS5连接
	// HandleRawTCP directly handles a raw TCP connection for transparent proxying.
	// It takes ownership of the inboundConn and is responsible for closing it.
	HandleRawTCP(inboundConn net.Conn, targetDest string)
	HandleUDPPacket(packet *UDPPacket, sessionKey string) error
	GetTrafficStats() TrafficStats

	// Initialize 启动一个本地监听器。此方法主要由移动端使用，保持不变。
	Initialize() error

	// InitializeForGateway 是一个专为 AppServer (网关模式) 设计的新的初始化方法。
	InitializeForGateway() error

	GetType() string
	CloseTunnel()
	GetListenerInfo() *ListenerInfo
	GetMetrics() *Metrics
	UpdateServer(newProfile *ServerProfile) error
	CheckHealth() error
}

// ServerState 封装了与单个服务器相关的所有信息：配置、实例和运行时状态。
// 这是系统中代表一个服务器通道的唯一事实来源。
type ServerState struct {
	Profile  *ServerProfile // 静态配置 (来自 servers.json)
	Instance TunnelStrategy // 动态的策略实例 (可能为 nil)

	// -- 运行时状态字段 --
	Health  HealthStatus // 健康检查状态 (Up/Down/Unknown)
	Metrics *Metrics     // 性能指标 (连接数, 延迟)
	ExitIP  string       // 健康检查获取的出口IP
}

// StateProvider 接口定义了一个提供实时后端状态的查询器。
// AppServer 将实现此接口，并将其注入 Dispatcher。
type StateProvider interface {
	GetServerStates() map[string]*ServerState
}

// Dispatcher 接口的 Dispatch 方法现在返回一个完整的 TunnelStrategy 实例
type Dispatcher interface {
	Dispatch(ctx context.Context, source net.Addr, target string) (TunnelStrategy, string, error)
}

// ProxyPoolStatusItem extends ProxyInfo with its current usage status within the application.
type ProxyPoolStatusItem struct {
	model.ProxyInfo
	Status  string `json:"status"`    // "Idle" or "In Use"
	InUseBy string `json:"in_use_by"` // Remarks of the server profile using this proxy, if any.
}

type HealthStatus int

const (
	StatusUnknown HealthStatus = iota // Default value
	StatusUp
	StatusDown
)

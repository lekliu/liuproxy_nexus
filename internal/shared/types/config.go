package types

// ServerProfile 定义了一个后端服务器的完整配置。
// 这是未来 configs/servers.json 文件的核心数据结构。
type ServerProfile struct {
	// --- 通用字段 ---
	ID      string `json:"id"`      // 唯一标识符 (例如 UUID)，由Dashboard生成和管理
	Remarks string `json:"remarks"` // 用户备注
	Type    string `json:"type"`    // 服务器类型: "goremote", "worker", "vless", "http"
	Active  bool   `json:"active"`  // 是否加入默认的HAProxy负载均衡池

	LocalPort int `json:"localPort,omitempty"`

	// --- HTTP 代理专属参数 ---
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`

	// --- 连接参数 ---
	Address string `json:"address"` // 服务器地址 (域名或IP)
	Port    int    `json:"port"`    // 服务器端口

	// --- 传输层协议 ---
	Network string `json:"network,omitempty"` // 传输协议: "ws", "grpc" 等, vless专属

	// --- WebSocket 参数 (用于 goremote, worker, vless+ws) ---
	Scheme string `json:"scheme,omitempty"` // "ws" or "wss"
	Path   string `json:"path,omitempty"`   // WebSocket路径, e.g., "/tunnel"
	Host   string `json:"host,omitempty"`   // WebSocket Host请求头, 用于CDN等场景

	// --- GoRemote 专属参数 ---
	Transport string `json:"transport,omitempty"` // 新增: "tcp" (默认) 或 "ws"
	Multiplex bool   `json:"multiplex,omitempty"` // 新增: 是否启用多路复用

	// --- gRPC 参数 (vless+grpc) ---
	GrpcServiceName string `json:"grpcServiceName"`
	GrpcMode        string `json:"grpcMode,omitempty"`
	GrpcAuthority   string `json:"grpcAuthority"`

	// --- Worker 专属参数 ---
	EdgeIP string `json:"edgeIP,omitempty"` // Worker优选IP

	// --- VLESS 专属参数  ---
	UUID        string `json:"uuid,omitempty"`
	Flow        string `json:"flow,omitempty"`
	Security    string `json:"security,omitempty"` // "tls" or "reality"
	SNI         string `json:"sni,omitempty"`      // TLS SNI
	Fingerprint string `json:"fingerprint,omitempty"`
	PublicKey   string `json:"publicKey,omitempty"` // REALITY公钥
	ShortID     string `json:"shortId,omitempty"`   // REALITY ShortID
}

// CommonConf 包含共有的配置
type CommonConf struct {
	Mode           string `ini:"mode"`
	MaxConnections int    `ini:"maxConnections"`
	BufferSize     int    `ini:"bufferSize"`
	Crypt          int    `ini:"crypt"`
}

// LocalConf 包含local模式特有的配置
type LocalConf struct {
	UnifiedPort int    `ini:"unified_port"`
	TProxyPort  int    `ini:"tproxy_port"` // <-- 新增
	WebPort     int    `ini:"web_port"`
	WebUser     string `ini:"web_user"`
	WebPassword string `ini:"web_password"`
}

// LogConf contains logging specific configuration
type LogConf struct {
	Level string `ini:"level"`
}

// GatewayConf 包含网关特有的配置
type GatewayConf struct {
	StickySessionMode string `ini:"sticky_session_mode"` // 粘性会话模式: disabled, global, conditional
	StickySessionTTL  int    `ini:"sticky_session_ttl"`  // 粘性会话的TTL (秒)
}

// Config 是local项目的统一配置结构体 (现在只包含行为配置)
type Config struct {
	CommonConf  `ini:"common"`
	LocalConf   `ini:"local"`
	LogConf     `ini:"log"`
	GatewayConf `ini:"Gateway"`
}

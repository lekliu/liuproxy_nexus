package model

import "time"

// ProxyInfo 定义了一个代理的完整信息，是整个模块的核心数据结构。
// 它在内存中使用，并通过API序列化为JSON，但通过FileStorage持久化为纯文本。
type ProxyInfo struct {
	// 核心信息
	ID   string `json:"id"` // 唯一ID, 使用 "ip:port"
	IP   string `json:"ip"`
	Port int    `json:"port"`

	// 元数据
	Source  string `json:"source"`            // 来源网站, e.g., "kuaidaili.com"
	Country string `json:"country,omitempty"` // 国家/地区 (内部的'|'已被Scraper替换为空格)

	// ScrapedProtocol 是代理源网站声称的协议 ("http", "socks5" 等)。
	// 它作为 Validator 的一个提示。
	ScrapedProtocol string `json:"scraped_protocol"`

	// VerifiedProtocol 是经过我们系统成功验证的协议。
	// 值为 "http" (表示支持CONNECT) 或 "socks5"。
	// 如果验证失败或未执行，此字段为空。
	VerifiedProtocol string `json:"verified_protocol"`

	// 健康状态与生命周期管理
	Latency      time.Duration `json:"latency"`       // 延迟, 0 表示测试失败或未测试
	LastChecked  time.Time     `json:"last_checked"`  // 上次检查时间
	NextChecked  time.Time     `json:"next_checked"`  // 下次计划检查时间
	FailureCount int           `json:"failure_count"` // 连续失败次数
	SuccessCount int           `json:"success_count"` // 连续成功次数
}

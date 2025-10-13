package dispatcher

import (
	"github.com/rs/zerolog/log"
	"liuproxy_gateway/internal/shared/settings"
	"liuproxy_gateway/internal/shared/types"
	"regexp"
	"strings"
	"sync"
	"time"
)

// StickyRecord 存储粘性会话的映射记录。
type StickyRecord struct {
	ServerID string    // 后端服务器的唯一ID
	Expiry   time.Time // 此记录的过期时间戳
}

// ClientActivityRecord 存储一个客户端IP的最后活动时间。
type ClientActivityRecord struct {
	LastSeen time.Time
}

// StickyManager 负责管理 (源IP, 目标主机) -> 后端 的粘性映射。
type StickyManager struct {
	mu           sync.RWMutex
	sessionCache sync.Map // key (string "ip:host") -> *StickyRecord
	clientCache  sync.Map // key (string "ip") -> *ClientActivityRecord
	ttl          time.Duration
	mode         string
	ruleMatchers []func(string) bool // 存储已编译的规则匹配函数
	cleanupStop  chan struct{}
}

// NewStickyManager 创建一个新的粘性会话管理器。
func NewStickyManager(cfg *settings.GatewaySettings) *StickyManager {
	var matchers []func(string) bool
	if cfg != nil && cfg.StickyRules != nil {
		matchers = make([]func(string) bool, len(cfg.StickyRules))
		for i, rule := range cfg.StickyRules {
			if strings.Contains(rule, "*") {
				pattern := strings.ReplaceAll(rule, ".", "\\.")
				pattern = strings.ReplaceAll(pattern, "*", ".*")
				if re, err := regexp.Compile("(?i)^" + pattern + "$"); err == nil {
					matchers[i] = re.MatchString
					continue
				}
			}
			lowerRule := strings.ToLower(rule)
			matchers[i] = func(host string) bool {
				return strings.ToLower(host) == lowerRule
			}
		}
	}

	mode := "disabled"
	ttl := 0
	if cfg != nil {
		mode = cfg.StickySessionMode
		ttl = cfg.StickySessionTTL
	}

	return &StickyManager{
		ttl:          time.Duration(ttl) * time.Second,
		mode:         mode,
		ruleMatchers: matchers,
		cleanupStop:  make(chan struct{}),
	}
}

// RecordClientActivity 记录一个客户端IP的活动。
func (sm *StickyManager) RecordClientActivity(clientIP string) {
	if sm.ttl <= 0 { // 如果TTL为0，则不记录任何内容
		return
	}
	record := &ClientActivityRecord{LastSeen: time.Now()}
	sm.clientCache.Store(clientIP, record)
}

// ShouldApply 根据当前模式和目标主机，决定是否应用粘性会话。
func (sm *StickyManager) ShouldApply(targetHost string) bool {
	if sm.ttl <= 0 {
		return false
	}

	switch sm.mode {
	case "disabled":
		return false
	case "global":
		return true
	case "conditional":
		for _, matcher := range sm.ruleMatchers {
			if matcher(targetHost) {
				return true
			}
		}
		return false
	default:
		return false // 默认安全，禁用
	}
}

// Get 查找一个有效的粘性记录。
// 如果找到且记录有效（未过期、后端健康），则续期并返回该记录。
// 否则返回 nil，并从缓存中删除无效记录。
func (sm *StickyManager) Get(key string, serverStates map[string]*types.ServerState) *StickyRecord {
	if sm.mode == "disabled" || sm.ttl <= 0 {
		return nil
	}

	value, ok := sm.sessionCache.Load(key)
	if !ok {
		return nil
	}

	record := value.(*StickyRecord)

	// 检查过期
	if time.Now().After(record.Expiry) {
		sm.sessionCache.Delete(key)
		return nil
	}

	// 检查后端健康状态
	state, exists := serverStates[record.ServerID]
	log.Debug().
		Str("sticky_key", key).
		Str("server_id", record.ServerID).
		Bool("state_exists", exists).
		Msg("StickyManager: Checking status for cached entry.")

	isValid := exists && state.Profile.Active && state.Health == types.StatusUp

	if !isValid {
		sm.sessionCache.Delete(key)
		return nil
	}

	// 续期并返回
	sm.mu.Lock()
	record.Expiry = time.Now().Add(sm.ttl)
	sm.mu.Unlock()

	return record
}

// Set 添加或更新一条粘性记录。
func (sm *StickyManager) Set(key string, serverID string) {
	if sm.mode == "disabled" || sm.ttl <= 0 {
		return
	}

	record := &StickyRecord{
		ServerID: serverID,
		Expiry:   time.Now().Add(sm.ttl),
	}
	sm.sessionCache.Store(key, record)
}

// Start 启动后台清理goroutine。
func (sm *StickyManager) Start() {
	if sm.mode == "disabled" || sm.ttl <= 0 {
		return
	}

	ticker := time.NewTicker(60 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				sm.cleanup()
			case <-sm.cleanupStop:
				ticker.Stop()
				return
			}
		}
	}()
}

// Stop 停止后台清理goroutine。
func (sm *StickyManager) Stop() {
	if sm.mode == "disabled" || sm.ttl <= 0 {
		return
	}
	// 避免在从未启动的情况下关闭channel导致panic
	if sm.cleanupStop != nil {
		select {
		case <-sm.cleanupStop:
			// 已经关闭
		default:
			close(sm.cleanupStop)
		}
	}
}

// cleanup 遍历缓存并移除所有过期的记录。
func (sm *StickyManager) cleanup() {
	now := time.Now()
	sm.sessionCache.Range(func(key, value interface{}) bool {
		record := value.(*StickyRecord)
		if now.After(record.Expiry) {
			sm.sessionCache.Delete(key)
		}
		return true
	})

	// 清理客户端IP缓存
	clientExpiry := now.Add(-sm.ttl) // 使用与粘性会话相同的TTL
	sm.clientCache.Range(func(key, value interface{}) bool {
		record := value.(*ClientActivityRecord)
		if record.LastSeen.Before(clientExpiry) {
			sm.clientCache.Delete(key)
		}
		return true
	})
}

// GetAllClientIPs 遍历缓存并返回一个唯一的客户端IP地址列表。
func (sm *StickyManager) GetAllClientIPs() []string {
	ipSet := make(map[string]struct{})
	sm.clientCache.Range(func(key, value interface{}) bool {
		ipSet[key.(string)] = struct{}{}
		return true
	})

	ips := make([]string, 0, len(ipSet))
	for ip := range ipSet {
		ips = append(ips, ip)
	}
	return ips
}

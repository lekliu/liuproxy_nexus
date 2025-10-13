// --- START OF NEW FILE internal/globalstate/state_manager.go ---
package globalstate

import (
	"sync"
)

// StatusManager 结构体用于管理全局状态。
// 它使用 RWMutex 来保护对状态字符串的并发读写。
type StatusManager struct {
	mu     sync.RWMutex
	status string
}

// 全局的状态管理器实例
var GlobalStatus = &StatusManager{status: "Initializing..."}

// Set 方法用于安全地更新状态。
func (sm *StatusManager) Set(newStatus string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.status = newStatus
}

// Get 方法用于安全地读取状态。
func (sm *StatusManager) Get() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.status
}

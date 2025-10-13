package settings

import (
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog/log"
	"os"
	"sync"
	"sync/atomic"
)

// SettingsManager 是运行时配置的核心管理器。
// 它线程安全，并使用原子操作和发布/订阅模式来处理配置的读取和热重载。
type SettingsManager struct {
	filePath    string
	settings    atomic.Value // 存储一个 *RuntimeSettings 指针，用于无锁读取
	subscribers map[string][]ConfigurableModule
	mu          sync.RWMutex // 用于保护 subscribers map 和文件写入操作
}

// NewSettingsManager 创建并初始化一个新的配置管理器。
// 它会立即从指定的路径加载配置，如果文件不存在，则会创建一个默认配置。
func NewSettingsManager(filePath string) (*SettingsManager, error) {
	sm := &SettingsManager{
		filePath:    filePath,
		subscribers: make(map[string][]ConfigurableModule),
	}

	if filePath == "" {
		sm.settings.Store(createDefaultSettings())
		//log.Info().Msg("SettingsManager initialized in-memory with default values (no-file mode).")
		return sm, nil
	}

	if err := sm.load(); err != nil {
		return nil, fmt.Errorf("failed to load initial settings: %w", err)
	}

	return sm, nil
}

// load 从磁盘加载 settings.json 文件。
// 如果文件不存在，它会初始化一个默认的空配置。
func (sm *SettingsManager) load() error {
	data, err := os.ReadFile(sm.filePath)
	settings := &RuntimeSettings{}

	if err != nil {
		if os.IsNotExist(err) {
			log.Warn().Str("path", sm.filePath).Msg("settings.json not found, creating with default values.")
			// 文件不存在，初始化默认结构
			settings = createDefaultSettings()
			// 尝试写入一次，以确保文件存在
			if err := sm.persist(settings); err != nil {
				return fmt.Errorf("failed to write default settings file: %w", err)
			}
		} else {
			return fmt.Errorf("failed to read settings file: %w", err)
		}
	} else {
		if err := json.Unmarshal(data, settings); err != nil {
			return fmt.Errorf("failed to parse settings.json: %w", err)
		}
		// 确保即使JSON中缺少某些模块，指针也不是nil
		ensureDefaultModules(settings)
	}

	sm.settings.Store(settings)
	return nil
}

// Register 将一个模块注册为特定配置主题的订阅者。
func (sm *SettingsManager) Register(moduleKey string, module ConfigurableModule) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.subscribers[moduleKey] = append(sm.subscribers[moduleKey], module)
}

// Get 返回当前运行时配置的一个快照。此操作是无锁的，非常高效。
func (sm *SettingsManager) Get() *RuntimeSettings {
	return sm.settings.Load().(*RuntimeSettings)
}

// Update 是更新配置的核心方法。它接收一个模块的原始JSON数据，
// 然后原子性地更新内存中的配置、持久化到磁盘，并通知所有相关订阅者。
func (sm *SettingsManager) Update(moduleKey string, newSettingsData json.RawMessage) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 1. 深拷贝当前的配置，以避免竞态条件
	currentSettings := sm.Get()
	newSettings := deepCopy(currentSettings)

	// 2. 将新的JSON数据反序列化到新配置的对应模块上
	targetModule := getModuleByKey(newSettings, moduleKey)
	if targetModule == nil {
		return fmt.Errorf("unknown settings module: %s", moduleKey)
	}
	if err := json.Unmarshal(newSettingsData, targetModule); err != nil {
		return fmt.Errorf("failed to parse JSON for module %s: %w", moduleKey, err)
	}

	// 3. 持久化到文件 (仅在 PC 模式下)
	if sm.filePath != "" {
		if err := sm.persist(newSettings); err != nil {
			return fmt.Errorf("failed to save updated settings to disk: %w", err)
		}
	}

	// 4. 原子地替换内存中的配置指针
	sm.settings.Store(newSettings)
	//log.Info().Str("module", moduleKey).Msg("Runtime settings updated and persisted.")

	// 5. 异步通知订阅者
	go sm.notify(moduleKey, targetModule)

	return nil
}

// persist 将完整的配置结构体写入到 settings.json 文件。
func (sm *SettingsManager) persist(settings *RuntimeSettings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sm.filePath, data, 0644)
}

// notify 异步地通知所有订阅了指定模块的模块。
func (sm *SettingsManager) notify(moduleKey string, newSettings interface{}) {
	sm.mu.RLock()
	subscribers, ok := sm.subscribers[moduleKey]
	sm.mu.RUnlock()

	if ok {
		log.Debug().Str("module", moduleKey).Int("subscribers", len(subscribers)).Msg("Notifying subscribers of settings update.")
		for _, sub := range subscribers {
			if err := sub.OnSettingsUpdate(moduleKey, newSettings); err != nil {
				log.Error().Err(err).Str("module", moduleKey).Msg("Error notifying subscriber.")
			}
		}
	}
}

// --- 辅助函数 ---

func deepCopy(s *RuntimeSettings) *RuntimeSettings {
	newS := *s
	if s.Gateway != nil {
		gwCopy := *s.Gateway
		newS.Gateway = &gwCopy
	}
	// ... 对其他模块执行相同的深度拷贝 ...
	return &newS
}

func getModuleByKey(s *RuntimeSettings, key string) interface{} {
	switch key {
	case "gateway":
		return s.Gateway
	case "routing":
		return s.Routing
	case "logging":
		return s.Logging
	case "firewall":
		return s.Firewall
	default:
		return nil
	}
}

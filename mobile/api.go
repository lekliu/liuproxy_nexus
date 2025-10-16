package mobile

import (
	"encoding/json"
	"fmt"
	"gopkg.in/ini.v1"
	"liuproxy_nexus/internal/shared/logger"
	"runtime/debug"
	"sync"

	"liuproxy_nexus/internal/app"
	"liuproxy_nexus/internal/shared/types"
)

var (
	// 全局变量，用于持有当前为移动端运行的唯一 AppServer 实例
	activeAppServer *app.AppServer
	instanceMutex   sync.Mutex
)

// --- StatsData 定义了从 Go 返回给移动端的数据结构 ---
type StatsData struct {
	ID                string `json:"id"`
	ActiveConnections int64  `json:"activeConnections"`
	Latency           int64  `json:"latency"`  // Latency in milliseconds
	Uplink            uint64 `json:"uplink"`   // Total uplink bytes
	Downlink          uint64 `json:"downlink"` // Total downlink bytes
}

// StartVPN is the main entry point for mobile clients.
// It starts the Go core in-memory, without any file I/O for configuration.
// iniContent: A string containing the content of a liuproxy.ini file.
// profilesJson: A JSON string representing an array of []*types.ServerProfile.
func StartVPN(iniContent, profilesJson string, routingRulesJson string) (port int, err error) {
	// Defer a panic handler to convert panics into errors, which is safer for CGo boundaries.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("go core panic: %v\n\n%s", r, debug.Stack())
			port = 0
		}
	}()

	instanceMutex.Lock()
	defer instanceMutex.Unlock()

	if activeAppServer != nil {
		return 0, fmt.Errorf("service is already running")
	}

	// 1. Parse iniContent string to get the configuration struct.
	cfg := new(types.Config)
	iniFile, err := ini.Load([]byte(iniContent))
	if err != nil {
		return 0, fmt.Errorf("failed to parse ini content: %w", err)
	}
	if err := iniFile.MapTo(cfg); err != nil {
		return 0, fmt.Errorf("failed to map ini content to config struct: %w", err)
	}

	// 2. 初始化日志系统
	if err := logger.Init(cfg.LogConf); err != nil {
		// Fallback to standard logger if initialization fails.
		fmt.Printf("Fatal: Failed to initialize logger: %v\n", err)
		return 0, fmt.Errorf("failed to initialize logger: %w", err)
	}

	logger.Debug().Msg("Configuring and starting Go core for mobile (in-memory)...")

	// 3. Unmarshal the server profiles from the JSON string.
	var profiles []*types.ServerProfile
	if err := json.Unmarshal([]byte(profilesJson), &profiles); err != nil {
		return 0, fmt.Errorf("failed to unmarshal profiles JSON: %w", err)
	}
	logger.Debug().Int("count", len(profiles)).Msg("Successfully unmarshalled profiles for mobile.")

	// 4. Create a new AppServer instance. File paths are empty as we are in memory mode.
	appServer := app.NewForMobile(cfg)

	// 5. Start the server in mobile mode, passing the in-memory profiles and rules.
	port, err = appServer.StartMobile(profiles, routingRulesJson)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to start app server in mobile mode")
		return 0, err
	}

	// 6. Store the active instance and return the listening port.
	activeAppServer = appServer
	logger.Debug().Int("port", port).Msgf("Go core started successfully, listening on port %d", port)

	return port, nil
}

// StopVPN stops the Go core.
func StopVPN() {
	instanceMutex.Lock()
	defer instanceMutex.Unlock()

	if activeAppServer != nil {
		logger.Debug().Msg("Stopping Go core for mobile...")
		activeAppServer.Stop()
		activeAppServer = nil
	}
}

// QueryStats 查询当前所有活动服务器的统计数据
// 返回一个 JSON 字符串，代表一个 []StatsData 数组
func QueryStats() (statsJson string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("go core panic in QueryStats: %v\n\n%s", r, debug.Stack())
			statsJson = "[]" // 在 panic 时返回一个空的 JSON 数组，避免 Kotlin 端崩溃
		}
	}()

	instanceMutex.Lock()
	defer instanceMutex.Unlock()

	if activeAppServer == nil {
		return "[]", nil // 服务未运行，返回空数组
	}

	serverStates := activeAppServer.GetServerStates()
	statsList := make([]StatsData, 0, len(serverStates))

	for _, state := range serverStates {
		if state.Profile != nil && state.Instance != nil {
			traffic := state.Instance.GetTrafficStats()
			statsList = append(statsList, StatsData{
				ID:                state.Profile.ID,
				ActiveConnections: state.Metrics.ActiveConnections,
				Latency:           state.Metrics.Latency,
				Uplink:            traffic.Uplink,
				Downlink:          traffic.Downlink,
			})
		}
	}

	statsBytes, err := json.Marshal(statsList)
	if err != nil {
		return "", fmt.Errorf("failed to marshal stats: %w", err)
	}

	return string(statsBytes), nil
}

// GetRecentTargets returns a JSON string of recently accessed targets, filtered and without port/duplicates.
func GetRecentTargets() (targetsJson string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("go core panic in GetRecentTargets: %v", r)
			targetsJson = "[]" // Return empty JSON array on panic
		}
	}()

	instanceMutex.Lock()
	defer instanceMutex.Unlock()

	if activeAppServer == nil {
		return "[]", nil // Return empty JSON array if server is not running
	}

	// This now correctly calls the internal AppServer method, which in turn
	// calls the dispatcher with filtering logic.
	targets := activeAppServer.GetRecentTargets()

	statsBytes, err := json.Marshal(targets)
	if err != nil {
		return "[]", fmt.Errorf("failed to marshal recent targets: %w", err)
	}
	return string(statsBytes), nil
}
